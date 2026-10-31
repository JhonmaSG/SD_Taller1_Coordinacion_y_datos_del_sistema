# Configuración de un nodo

1. Instalar Ubuntu Server 26.04.1 LTS sin GUI.
2. SSH por clave:
```bash
ssh-keygen -t ed25519
ssh-copy-id usuario@nodo
# /etc/ssh/sshd_config: PasswordAuthentication no
sudo systemctl restart ssh
```
3. Tailscale:
```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
tailscale ip -4
```
4. Verificación (E3): `scripts/verificar_conectividad.sh`
