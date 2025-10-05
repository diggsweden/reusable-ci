# shellcheck shell=sh
# shellcheck disable=SC2034  # credential_trace is read by the sourcing probe.
case $- in *x*)
	set +x
	credential_trace=1
	;;
*) credential_trace=0 ;; esac
unset rc_actions_id_token_request_url rc_actions_id_token_request_token rc_sigstore_id_token
rc_actions_id_token_request_url="${ACTIONS_ID_TOKEN_REQUEST_URL:-}"
rc_actions_id_token_request_token="${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}"
unset ACTIONS_ID_TOKEN_REQUEST_URL ACTIONS_ID_TOKEN_REQUEST_TOKEN
if [ -z "$rc_actions_id_token_request_url" ] || [ -z "$rc_actions_id_token_request_token" ]; then
	echo "FAIL: enable-openid-connect injected no token endpoint"
	exit 1
fi
