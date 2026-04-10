#!/usr/bin/env bash
# Bootstrap openclaw from your laptop.
#
# Reads .env, creates/updates Cloudflare DNS, issues an origin CA cert,
# writes the cert to ansible/secrets/, then runs the ansible playbook.
#
# Required env (in .env or exported):
#   CF_API_TOKEN, CF_ZONE, CLAW_HOSTNAME, CLAW_HOST
#
# Optional:
#   CLAW_SSH_USER (default: ubuntu)
#   TELEGRAM_BOT_TOKEN / TELEGRAM_ALLOWED_USER_IDS / ANTHROPIC_API_KEY
#     — if set, they will be pushed to the VM's /opt/openclaw/.env after
#       provisioning. If unset, provisioning still succeeds and you can run
#       `sudo /opt/openclaw/scripts/finish-setup.sh` on the VM later.
set -euo pipefail

cd "$(dirname "$0")/.."
REPO_ROOT="$(pwd)"

if [[ -f .env ]]; then
    # shellcheck disable=SC1091
    set -a; source .env; set +a
fi

: "${CF_API_TOKEN:?CF_API_TOKEN not set}"
: "${CF_ZONE:?CF_ZONE not set}"
: "${CLAW_HOSTNAME:?CLAW_HOSTNAME not set}"
: "${CLAW_HOST:?CLAW_HOST not set}"
CLAW_SSH_USER="${CLAW_SSH_USER:-ubuntu}"

SECRETS_DIR="${REPO_ROOT}/ansible/secrets"
mkdir -p "${SECRETS_DIR}"
chmod 700 "${SECRETS_DIR}"

api() {
    curl -fsS -H "Authorization: Bearer ${CF_API_TOKEN}" \
               -H "Content-Type: application/json" "$@"
}

echo "==> Fetching Cloudflare zone id for ${CF_ZONE}"
ZONE_ID=$(api "https://api.cloudflare.com/client/v4/zones?name=${CF_ZONE}" \
    | jq -r '.result[0].id')
if [[ -z "${ZONE_ID}" || "${ZONE_ID}" == "null" ]]; then
    echo "could not resolve zone ${CF_ZONE}" >&2
    exit 1
fi
echo "    zone_id=${ZONE_ID}"

echo "==> Upserting DNS A record ${CLAW_HOSTNAME} -> ${CLAW_HOST}"
EXISTING=$(api "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records?type=A&name=${CLAW_HOSTNAME}" \
    | jq -r '.result[0].id // empty')
PAYLOAD=$(jq -n --arg h "${CLAW_HOSTNAME}" --arg ip "${CLAW_HOST}" \
    '{type:"A",name:$h,content:$ip,ttl:1,proxied:true}')
if [[ -n "${EXISTING}" ]]; then
    api -X PUT -d "${PAYLOAD}" \
        "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records/${EXISTING}" >/dev/null
else
    api -X POST -d "${PAYLOAD}" \
        "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records" >/dev/null
fi
echo "    DNS OK"

CERT_FILE="${SECRETS_DIR}/claw.biswas.me.crt"
KEY_FILE="${SECRETS_DIR}/claw.biswas.me.key"

if [[ ! -s "${CERT_FILE}" || ! -s "${KEY_FILE}" ]]; then
    echo "==> Generating origin CA certificate via Cloudflare API"
    openssl req -new -newkey rsa:2048 -nodes \
        -keyout "${KEY_FILE}" \
        -subj "/CN=${CLAW_HOSTNAME}" \
        -out "${SECRETS_DIR}/claw.csr" >/dev/null 2>&1
    chmod 600 "${KEY_FILE}"

    CSR_JSON=$(jq -Rs . < "${SECRETS_DIR}/claw.csr")
    REQ=$(jq -n --argjson csr "${CSR_JSON}" --arg h "${CLAW_HOSTNAME}" \
        '{hostnames:[$h,("*." + ($h|split(".")[1:]|join(".")))],requested_validity:5475,request_type:"origin-rsa",csr:$csr}')
    RESP=$(api -X POST -d "${REQ}" "https://api.cloudflare.com/client/v4/certificates")
    CERT=$(echo "${RESP}" | jq -r '.result.certificate // empty')
    if [[ -z "${CERT}" ]]; then
        echo "failed to create origin certificate:" >&2
        echo "${RESP}" >&2
        exit 1
    fi
    printf '%s\n' "${CERT}" > "${CERT_FILE}"
    chmod 644 "${CERT_FILE}"
    rm -f "${SECRETS_DIR}/claw.csr"
    echo "    cert written to ${CERT_FILE}"
else
    echo "==> Reusing existing origin cert in ${SECRETS_DIR}"
fi

echo "==> Writing ansible inventory"
INV="${REPO_ROOT}/ansible/inventory/hosts.yml"
cat >"${INV}" <<EOF
all:
  hosts:
    openclaw:
      ansible_host: ${CLAW_HOST}
      ansible_user: ${CLAW_SSH_USER}
      ansible_ssh_private_key_file: ~/.ssh/id_ed25519
      ansible_python_interpreter: /usr/bin/python3
  vars:
    claw_hostname: ${CLAW_HOSTNAME}
EOF

echo "==> Ensuring ansible collections are installed"
ansible-galaxy collection install -q community.general community.docker ansible.posix

echo "==> Running ansible playbook"
cd "${REPO_ROOT}/ansible"
ansible-playbook -i inventory/hosts.yml playbooks/site.yml

if [[ -n "${TELEGRAM_BOT_TOKEN:-}" ]]; then
    echo "==> Pushing secrets to /opt/openclaw/.env on ${CLAW_HOST}"
    SECRETS_TMP=$(mktemp)
    trap 'rm -f "${SECRETS_TMP}"' EXIT
    cat >"${SECRETS_TMP}" <<EOF
# openclaw runtime config — generated from bootstrap.sh
TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN}
TELEGRAM_ALLOWED_USER_IDS=${TELEGRAM_ALLOWED_USER_IDS:-}
ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}
CLAUDE_MODEL=${CLAUDE_MODEL:-claude-sonnet-4-5}
EOF
    scp -q "${SECRETS_TMP}" "${CLAW_SSH_USER}@${CLAW_HOST}:/tmp/openclaw.env"
    ssh "${CLAW_SSH_USER}@${CLAW_HOST}" \
        "sudo install -o root -g root -m 600 /tmp/openclaw.env /opt/openclaw/.env \
         && rm -f /tmp/openclaw.env \
         && sudo docker compose -f /opt/openclaw/bot/docker-compose.yml up -d"
    echo "    bot restarted"
else
    echo
    echo "⚠️  TELEGRAM_BOT_TOKEN not set locally. Provisioning finished, but the"
    echo "   bot is NOT running. SSH to the VM and run:"
    echo "     sudo /opt/openclaw/scripts/finish-setup.sh"
fi

echo
echo "==> Done. Health check:"
echo "     curl https://${CLAW_HOSTNAME}/health"
