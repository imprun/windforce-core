package controlcli

import (
	"fmt"
	"io"
	"strings"
)

var wfCompletionCommands = map[string][]string{
	"":             {"auth", "context", "workspace", "source", "app", "release", "action", "run", "job", "provisioning", "api", "openapi", "completion", "version", "help"},
	"auth":         {"login", "switch", "status", "logout"},
	"context":      {"list", "show", "set", "use"},
	"workspace":    {"list", "show", "view", "use"},
	"source":       {"list", "register", "probe", "sync", "publish"},
	"app":          {"publish", "list", "show", "history", "source", "openapi"},
	"release":      {"list", "view", "activate", "rollback"},
	"action":       {"show", "schema"},
	"run":          {"create", "wait", "show", "view", "watch", "result", "cancel"},
	"job":          {"list", "show", "result", "logs", "cancel"},
	"provisioning": {"export", "apply"},
}

func writeCompletion(writer io.Writer, shell string) error {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "bash":
		_, err := fmt.Fprint(writer, wfBashCompletion)
		return err
	case "zsh":
		_, err := fmt.Fprint(writer, wfZshCompletion)
		return err
	case "fish":
		_, err := fmt.Fprint(writer, wfFishCompletion())
		return err
	case "powershell", "pwsh":
		_, err := fmt.Fprint(writer, wfPowerShellCompletion)
		return err
	default:
		return usageError{"usage: wf completion bash|zsh|fish|powershell"}
	}
}

const wfBashCompletion = `_wf_complete() {
  local current group words
  current="${COMP_WORDS[COMP_CWORD]}"
  group="${COMP_WORDS[1]}"
  if [[ ${COMP_CWORD} -eq 1 ]]; then
    words="auth context workspace source app release action run job provisioning api openapi completion version help"
  else
    case "${group}" in
      auth) words="login switch status logout" ;;
      context) words="list show set use" ;;
      workspace) words="list show view use" ;;
      source) words="list register probe sync publish" ;;
      app) words="publish list show history source openapi" ;;
      release) words="list view activate rollback" ;;
      action) words="show schema" ;;
      run) words="create wait show view watch result cancel" ;;
      job) words="list show result logs cancel" ;;
      provisioning) words="export apply" ;;
    esac
  fi
  COMPREPLY=( $(compgen -W "${words}" -- "${current}") )
}
complete -F _wf_complete wf
`

const wfZshCompletion = `#compdef wf
_wf() {
  local -a commands
  commands=(
    'auth:authenticate with a Cell'
    'context:manage connection contexts'
    'workspace:inspect and select workspaces'
    'source:manage low-level Git sources'
    'app:manage apps'
    'release:inspect and activate releases'
    'action:inspect actions and schemas'
    'run:create and inspect Runs'
    'job:inspect low-level Jobs'
    'provisioning:export or apply provisioning'
    'api:call a Control Plane endpoint'
    'openapi:print workspace OpenAPI'
    'completion:generate completion code'
    'version:print version'
    'help:show help'
  )
  _describe 'command' commands
}
compdef _wf wf
`

func wfFishCompletion() string {
	var builder strings.Builder
	for _, command := range wfCompletionCommands[""] {
		fmt.Fprintf(&builder, "complete -c wf -n '__fish_use_subcommand' -a %s\n", command)
	}
	for _, group := range []string{"auth", "context", "workspace", "source", "app", "release", "action", "run", "job", "provisioning"} {
		for _, command := range wfCompletionCommands[group] {
			fmt.Fprintf(&builder, "complete -c wf -n '__fish_seen_subcommand_from %s' -a %s\n", group, command)
		}
	}
	return builder.String()
}

const wfPowerShellCompletion = `Register-ArgumentCompleter -Native -CommandName wf -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  $tokens = @($commandAst.CommandElements | ForEach-Object { $_.Value })
  $groups = @{
    auth = @('login','switch','status','logout')
    context = @('list','show','set','use')
    workspace = @('list','show','view','use')
    source = @('list','register','probe','sync','publish')
    app = @('publish','list','show','history','source','openapi')
    release = @('list','view','activate','rollback')
    action = @('show','schema')
    run = @('create','wait','show','view','watch','result','cancel')
    job = @('list','show','result','logs','cancel')
    provisioning = @('export','apply')
  }
  if ($tokens.Count -le 2) {
    $values = @('auth','context','workspace','source','app','release','action','run','job','provisioning','api','openapi','completion','version','help')
  } else {
    $values = $groups[$tokens[1]]
  }
  $values | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
    [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
  }
}
`
