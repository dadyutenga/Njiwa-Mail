#!/usr/bin/env bash
# Njiwa-Mail QEMU/KVM validation lab. SIMULATED networking only: results are NOT production evidence.
#
#   lab.sh init                 create base image copies, ssh key, cloud-init seeds for VMs "mx" and "net"
#   lab.sh up NAME [online|isolated]   start a VM (online = slirp with internet for apt/docker; isolated = no outside access)
#   lab.sh down NAME            clean poweroff
#   lab.sh kill NAME            hard power-off (simulates power loss)
#   lab.sh snap NAME TAG        offline snapshot (VM must be stopped)       lab.sh restore NAME TAG
#   lab.sh ssh NAME [cmd...]    lab.sh scp SRC... NAME:DST (use ssh-friendly paths)   lab.sh status
#
# Only user-space QEMU with KVM and slirp/dgram networking: no root, no host bridges, no host packages installed.
set -euo pipefail
LAB=${NJIWA_LAB:-$HOME/.cache/njiwa-vmlab}
IMG=${NJIWA_BASE_IMG:-$LAB/dl-ubuntu2404/noble-server-cloudimg-amd64.img}
VMS=$LAB/vms; KEY=$LAB/lab_ed25519
HERE=$(cd "$(dirname "$0")" && pwd)
declare -A SSHPORT=([mx]=2201 [net]=2202 [mxf]=2203) LANIP=([mx]=10.77.0.10 [net]=10.77.0.20 [mxf]=10.77.0.10)
declare -A LPORT=([mx]=5701 [net]=5702 [mxf]=5701) RPORT=([mx]=5702 [net]=5701 [mxf]=5702) MEM=([mx]=${MX_MEM:-4096} [net]=1536 [mxf]=${MX_MEM:-4096})
declare -A HOST=([mx]=mailout.njiwa.test [net]=lab-net.njiwa.test [mxf]=mailout.njiwa.test)
mac() { printf '52:54:00:%s:%s:%s' "$1" "$2" "$3"; }
ssh_opts=(-i "$KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5)

init() {
  mkdir -p "$VMS"
  [ -f "$KEY" ] || ssh-keygen -q -t ed25519 -N '' -C njiwa-lab -f "$KEY"
  for n in mx net; do
    [ -f "$VMS/$n.qcow2" ] || { qemu-img create -q -f qcow2 -b "$IMG" -F qcow2 "$VMS/$n.qcow2" 25G; }
    local d=$VMS/seed-$n; mkdir -p "$d"
    cat >"$d/meta-data" <<M
instance-id: njiwa-lab-$n
local-hostname: ${HOST[$n]%%.*}
M
    cat >"$d/user-data" <<U
#cloud-config
hostname: ${HOST[$n]%%.*}
fqdn: ${HOST[$n]}
manage_etc_hosts: true
ssh_pwauth: false
users:
  - name: lab
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys: ["$(cat "$KEY.pub")"]
write_files:
  - path: /etc/systemd/resolved.conf.d/lab.conf
    content: |
      [Resolve]
      DNS=10.0.2.3
      Domains=~.
      FallbackDNS=
  - path: /usr/local/sbin/lab-mode
    permissions: "0755"
    content: |
      #!/bin/sh
      # lab-mode online|isolated: which DNS the guest (and Docker containers it starts) uses
      case "\$1" in online) dns=10.0.2.3;; isolated) dns=10.77.0.20;; *) echo usage: lab-mode online|isolated >&2; exit 2;; esac
      printf '[Resolve]\nDNS=%s\nDomains=~.\nFallbackDNS=\n' "\$dns" > /etc/systemd/resolved.conf.d/lab.conf
      systemctl restart systemd-resolved
runcmd:
  - systemctl disable --now unattended-upgrades.service apt-daily.timer apt-daily-upgrade.timer || true
U
    cat >"$d/network-config" <<N
version: 2
ethernets:
  wan0:
    match: {macaddress: "$(mac 12 34 "$( [ $n = mx ] && echo 01 || echo 02)")"}
    set-name: wan0
    dhcp4: true
    dhcp4-overrides: {use-dns: false}
  lab0:
    match: {macaddress: "$(mac 77 00 "$( [ $n = mx ] && echo 10 || echo 20)")"}
    set-name: lab0
    addresses: [${LANIP[$n]}/24]
N
    genisoimage -quiet -output "$VMS/seed-$n.iso" -volid cidata -joliet -rock "$d/user-data" "$d/meta-data" "$d/network-config"
  done
}

pidf() { echo "$VMS/$1.pid"; }
alive() { [ -f "$(pidf "$1")" ] && kill -0 "$(cat "$(pidf "$1")")" 2>/dev/null; }

up() {
  local n=$1 mode=${2:-isolated} restrict=on
  alive "$n" && { echo "$n already running"; return; }
  [ "$mode" = online ] && restrict=off
  local idx lidx seed=$n; case $n in net) idx=02; lidx=20 ;; *) idx=01; lidx=10 ;; esac; [ "$n" = mxf ] && seed=mx
  qemu-system-x86_64 -name "njiwa-lab-$n" -machine q35,accel=kvm -cpu host -smp 2 -m "${MEM[$n]}" \
    -display none -daemonize -pidfile "$(pidf "$n")" -serial "file:$VMS/$n.serial.log" \
    -monitor "unix:$VMS/$n.mon,server,nowait" \
    -drive "file=$VMS/$n.qcow2,if=virtio,cache=writeback,format=qcow2" \
    -drive "file=$VMS/seed-$seed.iso,if=virtio,format=raw,readonly=on" \
    -netdev "user,id=wan,restrict=$restrict,hostfwd=tcp:127.0.0.1:${SSHPORT[$n]}-:22" \
    -device "virtio-net-pci,netdev=wan,mac=$(mac 12 34 "$idx")" \
    -netdev "dgram,id=lan,local.type=inet,local.host=127.0.0.1,local.port=${LPORT[$n]},remote.type=inet,remote.host=127.0.0.1,remote.port=${RPORT[$n]}" \
    -device "virtio-net-pci,netdev=lan,mac=$(mac 77 00 "$lidx")"
  echo "$mode" >"$VMS/$n.mode"
  for _ in $(seq 1 90); do
    ssh "${ssh_opts[@]}" -p "${SSHPORT[$n]}" lab@127.0.0.1 "cloud-init status --wait >/dev/null 2>&1; sudo lab-mode $mode" 2>/dev/null && { echo "$n up ($mode)"; return; }
    sleep 2
  done
  echo "$n did not become reachable; see $VMS/$n.serial.log" >&2; return 1
}

down() { ssh "${ssh_opts[@]}" -p "${SSHPORT[$1]}" lab@127.0.0.1 'sudo systemctl poweroff' 2>/dev/null || true
  for _ in $(seq 1 60); do alive "$1" || { echo "$1 stopped"; return; }; sleep 1; done
  echo "$1 still running" >&2; return 1; }
killvm() { alive "$1" && kill -9 "$(cat "$(pidf "$1")")"; sleep 1; echo "$1 killed (power loss)"; }
snap() { alive "$1" && { echo "stop $1 first" >&2; exit 1; }; qemu-img snapshot -c "$2" "$VMS/$1.qcow2"; }
restore() { alive "$1" && { echo "stop $1 first" >&2; exit 1; }; qemu-img snapshot -a "$2" "$VMS/$1.qcow2"; }
status() { for n in mx net mxf; do if alive $n; then echo "$n running ($(cat "$VMS/$n.mode" 2>/dev/null))"; else echo "$n stopped"; fi; done; }

cmd=${1:-}; shift || true
case $cmd in
  init) init ;;
  up) up "$@" ;;
  down) down "$1" ;;
  kill) killvm "$1" ;;
  snap) snap "$@" ;;
  restore) restore "$@" ;;
  status) status ;;
  # fresh: a standalone copy of mx at snapshot TAG (default pkgs) that takes mx's identity; run it only while mx is stopped
  fresh) alive mx && { echo "stop mx first (same IP)" >&2; exit 1; }; alive mxf && { echo "stop mxf first" >&2; exit 1; }
         rm -f "$VMS/mxf.qcow2"; qemu-img convert -O qcow2 -l "snapshot.name=${1:-pkgs}" "$VMS/mx.qcow2" "$VMS/mxf.qcow2"; echo "mxf created from mx snapshot ${1:-pkgs}" ;;
  ssh) n=$1; shift; exec ssh "${ssh_opts[@]}" -p "${SSHPORT[$n]}" lab@127.0.0.1 "$@" ;;
  scp) dst=${*: -1}; n=${dst%%:*}; p=${dst#*:}; set -- "${@:1:$#-1}"
       exec scp -r "${ssh_opts[@]}" -P "${SSHPORT[$n]}" "$@" "lab@127.0.0.1:$p" ;;
  *) sed -n 2,12p "$0"; exit 2 ;;
esac
