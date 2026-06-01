package integration_test

import "fmt"

func codexTaskConfig(fakeCodex string) string {
	return fmt.Sprintf(`
[task_types.codex]
command = %q
`, fakeCodex)
}

func codexTaskConfigWithTitle(fakeCodex string, titleTemplate string) string {
	return fmt.Sprintf(`
[task_types.codex]
command = %q
title_template = %q
`, fakeCodex, titleTemplate)
}

func codexTaskConfigWithTitleHook(fakeCodex string, titleTemplate string, titleHookCommand string) string {
	return fmt.Sprintf(`
title_hook_command = %q

[task_types.codex]
command = %q
title_template = %q
`, titleHookCommand, fakeCodex, titleTemplate)
}
