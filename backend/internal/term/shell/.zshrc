ZDOTDIR=$__jd_user_dir
[[ -r $ZDOTDIR/.zshrc ]] && source "$ZDOTDIR/.zshrc"
if (( ! $+functions[compdef] )); then
  autoload -Uz compinit
  compinit
fi
zmodload zsh/complist
zstyle ':completion:*' menu select
bindkey '^I' complete-word
__jd_prompt() { PROMPT=$'%F{cyan}%~%f\n%F{cyan}❯%f '; RPROMPT=''; }
precmd_functions+=(__jd_prompt)
__jd_prompt
unset __jd_config_dir __jd_user_dir
