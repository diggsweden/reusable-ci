# shellcheck shell=sh disable=SC2034,SC2154
credential_trace=0
case $- in
*x*)
	credential_trace=1
	set +x
	;;
esac
stored="$(jq -er --arg registry "$registry" '
  .auths[$registry].auth //
  .auths["https://" + $registry].auth //
  .auths["http://" + $registry].auth
' auth.json 2>/dev/null || true)"
if [ -z "$stored" ]; then
	echo "FAIL: container login wrote no usable credential"
	exit 1
fi
