#!/usr/bin/env bash
set -euo pipefail

# Ubuntu 64-bit (amd64) Installer for bacnet-gw systemd service
# Usage on Ubuntu server:
#   chmod +x install-ubuntu.sh
#   sudo ./install-ubuntu.sh

INSTALL_DIR="/opt/bacnet-gw"
SERVICE_USER="bacnetgw"
SERVICE_FILE="/etc/systemd/system/bacnet-gw.service"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ "${EUID}" -ne 0 ]]; then
  echo "Error: Please run as root (sudo ./install-ubuntu.sh)" >&2
  exit 1
fi

# Locate compiled linux binary matching host CPU architecture (supports bundle dir or repo deploy/ dir)
ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
  x86_64|amd64) GO_ARCH="amd64" ;;
  aarch64|arm64) GO_ARCH="arm64" ;;
  armv7*|armhf) GO_ARCH="arm" ;;
  *) GO_ARCH="${ARCH_RAW}" ;;
esac

BIN_SRC=""
for candidate in \
  "${SCRIPT_DIR}/bacnet-gw" \
  "${SCRIPT_DIR}/bacnet-gw-linux-${GO_ARCH}" \
  "${SCRIPT_DIR}/../dist/linux-${GO_ARCH}/bacnet-gw" \
  "${SCRIPT_DIR}/../dist/bacnet-gw-linux-${GO_ARCH}" \
  "${SCRIPT_DIR}/../bacnet-gw-linux-${GO_ARCH}"; do
  if [[ -f "${candidate}" ]]; then
    BIN_SRC="${candidate}"
    break
  fi
done

if [[ -z "${BIN_SRC}" ]]; then
  echo "Error: Could not find compiled bacnet-gw binary for linux/${GO_ARCH} next to install-ubuntu.sh or in ../dist/" >&2
  exit 1
fi

echo "==> 1. Creating system user '${SERVICE_USER}' (if needed)..."
if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi

echo "==> 2. Preparing ${INSTALL_DIR}..."
mkdir -p "${INSTALL_DIR}"

# Stop service cleanly before replacing binary if already running
if systemctl is-active --quiet bacnet-gw 2>/dev/null; then
  echo "    Stopping running bacnet-gw.service before binary upgrade..."
  systemctl stop bacnet-gw
fi

install -m 0755 "${BIN_SRC}" "${INSTALL_DIR}/bacnet-gw"

# Copy visual assets for the Digital Twin Web Dashboard (:8090) if present
for asset_dir in "${SCRIPT_DIR}" "${SCRIPT_DIR}/.."; do
  if [[ -f "${asset_dir}/dashboard.jpeg" ]]; then
    install -m 0644 "${asset_dir}/dashboard.jpeg" "${INSTALL_DIR}/dashboard.jpeg"
  fi
  if [[ -f "${asset_dir}/bacnet devices.jpeg" ]]; then
    install -m 0644 "${asset_dir}/bacnet devices.jpeg" "${INSTALL_DIR}/bacnet devices.jpeg"
  fi
  if [[ -f "${asset_dir}/config.example.yaml" ]]; then
    install -m 0644 "${asset_dir}/config.example.yaml" "${INSTALL_DIR}/config.example.yaml"
  fi
done

if [[ ! -f "${INSTALL_DIR}/config.yaml" ]]; then
  if [[ -f "${INSTALL_DIR}/config.example.yaml" ]]; then
    cp "${INSTALL_DIR}/config.example.yaml" "${INSTALL_DIR}/config.yaml"
    echo "    Created initial ${INSTALL_DIR}/config.yaml from config.example.yaml"
  fi
else
  echo "    Preserving existing ${INSTALL_DIR}/config.yaml"
fi

chown -R root:"${SERVICE_USER}" "${INSTALL_DIR}"
chmod 0750 "${INSTALL_DIR}"
if [[ -f "${INSTALL_DIR}/config.yaml" ]]; then
  chmod 0640 "${INSTALL_DIR}/config.yaml"
fi
if [[ -f "${INSTALL_DIR}/.env" ]]; then
  chmod 0640 "${INSTALL_DIR}/.env"
fi

echo "==> 3. Opening firewall for BACnet/IP UDP 47808 (if UFW is active)..."
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
  ufw allow 47808/udp comment "BACnet/IP Gateway (0xBAC0)" >/dev/null || true
  echo "    Allowed 47808/udp in UFW"
else
  echo "    UFW not active (skipped)"
fi

echo "==> 4. Installing systemd service ${SERVICE_FILE}..."
install -m 0644 "${SCRIPT_DIR}/bacnet-gw.service" "${SERVICE_FILE}"
systemctl daemon-reload
systemctl enable bacnet-gw
systemctl restart bacnet-gw

echo ""
echo "==> Done! bacnet-gw.service is enabled and running."
echo "    Config file : ${INSTALL_DIR}/config.yaml"
echo "    Scan subnet : ${INSTALL_DIR}/bacnet-gw -list-subnets"
echo "                  ${INSTALL_DIR}/bacnet-gw -subnet 192.168.0.0/24 -scan"
echo "    Live logs   : sudo journalctl -u bacnet-gw -f"
echo ""
systemctl --no-pager --full status bacnet-gw || true

