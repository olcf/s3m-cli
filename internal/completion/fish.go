package completion

import "fmt"

//nolint:dupword
const fishTemplate = `# %[1]s fish shell completion

function __fish_%[1]s_complete
    set -l cmd (commandline -opc)
    set -l cur (commandline -ct)

    if test (count $cmd) -eq 0
        return
    end

    if test -n "$cur"
        if string match -r '^-' -- "$cur"
            set cmd $cmd $cur
        end
    end

    set -a cmd --generate-shell-completion

    command env SHELL=fish $cmd 2>/dev/null
end

complete -c %[1]s -f -a "(__fish_%[1]s_complete)"
`

func GenerateFish(appName string) string {
	return fmt.Sprintf(fishTemplate, appName)
}
