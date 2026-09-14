# shellcheck shell=sh
# shellcheck disable=SC2034  # credential_trace is read by the sourcing probe.
case $- in *x*)
	set +x
	credential_trace=1
	;;
*) credential_trace=0 ;; esac
unset rc_sigstore_id_token
rc_sigstore_id_token="${SIGSTORE_ID_TOKEN:-}"
unset SIGSTORE_ID_TOKEN
if [ -z "$rc_sigstore_id_token" ]; then
	echo "FAIL: the runner minted no id_token, so there is no identity to sign with"
	exit 1
fi
