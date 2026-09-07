# Preserve the operator's zsh setup while choosing our final interactive hook.
typeset -g __jd_config_dir=$ZDOTDIR
typeset -g __jd_user_dir=$JD_ORIGINAL_ZDOTDIR
unset JD_ORIGINAL_ZDOTDIR
[[ -r $__jd_user_dir/.zshenv ]] && source "$__jd_user_dir/.zshenv"
ZDOTDIR=$__jd_config_dir
