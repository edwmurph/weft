package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/edwmurph/weft/internal/config"
	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/supervisor"
)

type processInfo struct {
	PID     int
	PPID    int
	RSSKB   int64
	Command string
}

type memoryDoctorReport struct {
	RuntimeDir             string
	SupervisorPID          int
	SupervisorFound        bool
	SupervisorRSSKB        int64
	TaskProcessCount       int
	TaskProcessRSSKB       int64
	TaskCount              int
	WeftSupervisorCount    int
	WeftSupervisorRSSKB    int64
	OtherSupervisorCount   int
	OtherSupervisorRSSKB   int64
	LargestOtherSupervisor processInfo
}

func doctorMemory(output io.Writer) error {
	rt, _, store, err := resolveRuntime()
	if err != nil {
		return err
	}
	st, stateErr := store.Read()
	pid, pidErr := readRuntimePID(supervisor.PIDPath(rt))
	processes, processErr := listProcesses()

	fmt.Fprintln(output, "Weft memory doctor")
	fmt.Fprintf(output, "info runtime dir: %s\n", rt.Dir)
	if stateErr == nil {
		fmt.Fprintf(output, "info state tasks: %d\n", len(st.Tasks))
	} else {
		fmt.Fprintf(output, "warn state unavailable: %v\n", stateErr)
	}
	if _, err := supervisor.Status(rt); err == nil {
		fmt.Fprintln(output, "ok supervisor: running")
	} else {
		fmt.Fprintf(output, "info supervisor: not running (%v)\n", err)
	}
	if pidErr != nil {
		fmt.Fprintf(output, "info supervisor pid: unavailable (%v)\n", pidErr)
	}
	if processErr != nil {
		fmt.Fprintf(output, "warn process list unavailable: %v\n", processErr)
		return nil
	}

	report := buildMemoryDoctorReport(rt, st, pid, processes)
	renderMemoryDoctorReport(output, report)
	return nil
}

func buildMemoryDoctorReport(rt config.Runtime, st state.State, supervisorPID int, processes []processInfo) memoryDoctorReport {
	report := memoryDoctorReport{
		RuntimeDir:    rt.Dir,
		SupervisorPID: supervisorPID,
		TaskCount:     len(st.Tasks),
	}
	children := processChildren(processes)
	for _, process := range processes {
		if process.PID == supervisorPID {
			report.SupervisorFound = true
			report.SupervisorRSSKB = process.RSSKB
		}
		if isWeftSupervisorProcess(process) {
			report.WeftSupervisorCount++
			report.WeftSupervisorRSSKB += process.RSSKB
			if process.PID != supervisorPID {
				report.OtherSupervisorCount++
				report.OtherSupervisorRSSKB += process.RSSKB
				if process.RSSKB > report.LargestOtherSupervisor.RSSKB {
					report.LargestOtherSupervisor = process
				}
			}
		}
	}
	if supervisorPID > 0 {
		for _, child := range processDescendants(supervisorPID, children) {
			report.TaskProcessCount++
			report.TaskProcessRSSKB += child.RSSKB
		}
	}
	return report
}

func renderMemoryDoctorReport(output io.Writer, report memoryDoctorReport) {
	if report.SupervisorPID > 0 {
		if report.SupervisorFound {
			fmt.Fprintf(output, "info supervisor rss: %s (pid %d)\n", formatRSSKB(report.SupervisorRSSKB), report.SupervisorPID)
		} else {
			fmt.Fprintf(output, "warn supervisor pid file points to a missing process: %d\n", report.SupervisorPID)
		}
	}
	fmt.Fprintf(output, "info task process rss: %s (%d descendant process(es))\n", formatRSSKB(report.TaskProcessRSSKB), report.TaskProcessCount)
	fmt.Fprintf(output, "info Weft supervisors: %d process(es), %s RSS total\n", report.WeftSupervisorCount, formatRSSKB(report.WeftSupervisorRSSKB))
	if report.OtherSupervisorCount > 0 {
		fmt.Fprintf(output, "warn other Weft supervisors: %d process(es), %s RSS total outside this runtime\n", report.OtherSupervisorCount, formatRSSKB(report.OtherSupervisorRSSKB))
		if report.LargestOtherSupervisor.PID != 0 {
			fmt.Fprintf(output, "info largest other supervisor: pid %d, %s RSS, %s\n", report.LargestOtherSupervisor.PID, formatRSSKB(report.LargestOtherSupervisor.RSSKB), trimProcessCommand(report.LargestOtherSupervisor.Command))
		}
	} else {
		fmt.Fprintln(output, "ok other Weft supervisors: none")
	}
}

func listProcesses() ([]processInfo, error) {
	cmd := exec.Command("ps", "-axo", "pid=,ppid=,rss=,command=")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseProcessList(string(out)), nil
}

func parseProcessList(output string) []processInfo {
	processes := []processInfo{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		rss, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		processes = append(processes, processInfo{
			PID:     pid,
			PPID:    ppid,
			RSSKB:   rss,
			Command: strings.Join(fields[3:], " "),
		})
	}
	return processes
}

func readRuntimePID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func isWeftSupervisorProcess(process processInfo) bool {
	fields := strings.Fields(process.Command)
	if len(fields) < 2 {
		return false
	}
	return filepath.Base(fields[0]) == "weft" && fields[1] == supervisor.CommandName
}

func processChildren(processes []processInfo) map[int][]processInfo {
	children := map[int][]processInfo{}
	for _, process := range processes {
		children[process.PPID] = append(children[process.PPID], process)
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			return children[parent][i].PID < children[parent][j].PID
		})
	}
	return children
}

func processDescendants(pid int, children map[int][]processInfo) []processInfo {
	var out []processInfo
	stack := append([]processInfo(nil), children[pid]...)
	for len(stack) > 0 {
		next := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, next)
		stack = append(stack, children[next.PID]...)
	}
	return out
}

func formatRSSKB(kb int64) string {
	return fmt.Sprintf("%.1f MB", float64(kb)/1024)
}

func trimProcessCommand(command string) string {
	command = strings.TrimSpace(command)
	if len(command) <= 140 {
		return command
	}
	return command[:137] + "..."
}
