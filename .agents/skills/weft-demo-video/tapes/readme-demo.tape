Output "/tmp/weft-demo-video/weft-demo-readme-vhs.mp4"

Require weft
Require go
Require ffmpeg
Require ttyd

Set Shell "zsh"
Set Width 2560
Set Height 1440
Set FontSize 22
Set FontFamily "Monaco"
Set LineHeight 1.28
Set LetterSpacing 0
Set Padding 18
Set MarginFill "#1d262a"
Set Framerate 25
Set TypingSpeed 45ms
Set PlaybackSpeed 1.0
Set WaitTimeout 30s
Set Theme {"name":"iTerm2 Default Profile","background":"#1d262a","foreground":"#e7ebed","cursor":"#eaeaea","selectionBackground":"#4e6a78","black":"#435b67","red":"#fc3841","green":"#5cf19e","yellow":"#fed032","blue":"#37b6ff","magenta":"#fc226e","cyan":"#59ffd1","white":"#ffffff","brightBlack":"#a1b0b8","brightRed":"#fc746d","brightGreen":"#adf7be","brightYellow":"#fee16c","brightBlue":"#70cfff","brightMagenta":"#fc669b","brightCyan":"#9affe6","brightWhite":"#ffffff"}

Env TERM "xterm-256color"
Env COLORTERM "truecolor"
Env FORCE_COLOR "1"
Env CLICOLOR_FORCE "1"

Type "weft"
Enter
Wait+Screen@30s /New task/
Sleep 3s

# Codex task 1: create, prompt, return, then group via the move form.
Type "n"
Sleep 2s
Down
Sleep 1s
Down
Sleep 1s
Enter
Wait+Screen@60s /OpenAI Codex/
Sleep 2s
Type "Find one tiny TUI rendering cleanup and run a narrow check."
Sleep 2s
Enter
Sleep 2s
Ctrl+b
Wait+Screen@30s /Workspaces/
Sleep 2s
Type "m"
Sleep 2s
Type "refactor"
Sleep 2s
Enter
Sleep 2s

# Shell task: select Shell, run a real narrow check, return, then group it.
Type "n"
Sleep 2s
Right
Sleep 2s
Down
Sleep 1s
Down
Sleep 1s
Enter
Wait+Screen@30s /Task Console/
Sleep 2s
Type "go test ./internal/titlehook"
Sleep 2s
Enter
Sleep 2s
Ctrl+b
Wait+Screen@30s /Workspaces/
Sleep 2s
Type "m"
Sleep 2s
Type "checks"
Sleep 2s
Enter
Sleep 2s

# Codex task 2: plan mode, prompt, return, then group via the move form.
Type "n"
Sleep 2s
Down
Sleep 1s
Down
Sleep 1s
Enter
Wait+Screen@60s /OpenAI Codex/
Sleep 2s
Shift+Tab
Sleep 2s
Type "Plan README demo upkeep. Do not edit files."
Sleep 2s
Enter
Sleep 2s
Ctrl+b
Wait+Screen@30s /Workspaces/
Sleep 2s
Type "m"
Sleep 2s
Type "refactor"
Sleep 2s
Enter
Sleep 3s

# Show parallel work and the help overlay at human-readable pace.
Down
Sleep 3s
Down
Sleep 3s
Up
Sleep 3s
Type "?"
Sleep 3s
Escape
Sleep 2s
Down
Sleep 4s
