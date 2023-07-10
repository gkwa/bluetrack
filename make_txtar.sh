set -e
set -u
set -x

rm -rf /tmp/bluetrack
mkdir -p /tmp/bluetrack

declare -a files=(
    main.go
    firewall.yaml
    sg_rules.tf
    stderr.log
)

for file in ${files[@]}; do
  cp $file /tmp/bluetrack
done

txtar-c /tmp/bluetrack | pbcopy
