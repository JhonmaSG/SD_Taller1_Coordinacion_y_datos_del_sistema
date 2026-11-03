# Configuración de un nodo
Guía paso a paso para conectarse por SSH con llave pública a una VM
(Ubuntu Server) dentro de una tailnet, y después deshabilitar el acceso por
contraseña.

**Datos de ejemplo** (reemplázalos por los tuyos):
 
| Dato | Ejemplo |
|---|---|
| Usuario de la VM | `node1` |
| IP de la VM en la tailnet | `100.83.114.76` |
| Nombre de la llave | `id_ed25519_vm` |
| Alias SSH | `nodo1` |
 
## Qué se hace en cada equipo
 
| Equipo | Qué se hace |
|---|---|
| **Tu PC** | Generar la llave, copiarla a la VM, conectarte |
| **La VM** | Servidor SSH activo, Tailscale conectado, recibir la llave **pública** |
 
> La llave **privada** nunca sale de tu PC. Solo se copia la `.pub`.

---
 
## 1. Requisitos previos
- Instalar Ubuntu Server 26.04.1 LTS sin GUI.
- Tailscale instalado y conectado en la **VM** y en tu **PC**, con la **misma cuenta**.

Tailscale:
```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
tailscale ip -4
```
- Servidor SSH instalado y corriendo en la VM.
### En la VM
 
```bash
sudo apt update
sudo apt install -y openssh-server
sudo systemctl enable --now ssh
sudo systemctl status ssh
tailscale ip -4        # anota la IP 100.x.y.z
whoami                 # anota el usuario
```
 
### En tu PC
 
```bash
tailscale status             # la VM debe aparecer
ping ip
```

### O usar el Script de Verificación: 

Verificación (E3): `scripts/verificar_conectividad.sh`
 
> No uses `tailscale up --ssh` si el requisito es acceso por llave pública:
> Tailscale SSH intercepta las conexiones y no pasan por `sshd`.
> Para desactivarlo: `sudo tailscale set --ssh=false`.
 
---
 
## 2. Generar la llave (en tu PC)
 
```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_vm -C "acceso-nodos"
```
 
- `-f` evita sobrescribir otras llaves (por ejemplo la de GitHub).
- Define una **passphrase** larga y que recuerdes.
Se crean:
 
- `~/.ssh/id_ed25519_vm` → privada (no compartir).
- `~/.ssh/id_ed25519_vm.pub` → pública (va a la VM).
---
 
## 3. Copiar la llave pública a la VM (desde tu PC)
 
```bash
ssh-copy-id -i ~/.ssh/id_ed25519_vm.pub node1@100.83.114.76
```
 
Verás:
 
1. Pregunta por la huella → escribe `yes`.
2. Pide la contraseña de `node1` (solo esta vez).
3. Confirma con `Number of key(s) added: 1`.
**Windows (PowerShell)**, donde no existe `ssh-copy-id`:
 
```powershell
type $env:USERPROFILE\.ssh\id_ed25519_vm.pub | ssh node1@100.83.114.76 "mkdir -p ~/.ssh && chmod 700 ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
```

Como segunda opción si falla el comando anterior, (En Windows) usar:
```powershell
mkdir "$env:USERPROFILE\.ssh" -Force
```

```powershell
ssh-keygen -t ed25519 -f "$env:USERPROFILE\.ssh\id_ed25519_vm" -C "acceso-nodos"
```

Verificar si fue creado exitosamente la llave:
```powershell
(optional) dir "$env:USERPROFILE\.ssh"
```

Volver a ejecutar el comando:
```powershell
type $env:USERPROFILE\.ssh\id_ed25519_vm.pub | ssh node1@100.83.114.76 "mkdir -p ~/.ssh && chmod 700 ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
```
 
### Si sale `Connection refused`
 
- Comprueba que la IP es la de la VM (`tailscale ip -4` dentro de la VM).
- Comprueba que SSH escucha: `sudo ss -tlnp | grep :22`.
- Revisa firewall: `sudo ufw status` y `sudo iptables -L INPUT -n --line-numbers`.
---
 
## 4. Probar el acceso con llave
 
```bash
ssh -i ~/.ssh/id_ed25519_vm node1@100.83.114.76
```
 
Debe pedir la **passphrase de la llave**, no la contraseña del usuario.
 
---
 
## 5. Deshabilitar el acceso por contraseña
 
> **Hazlo solo después de confirmar que el paso 4 funciona.**
> Mantén abierta una sesión SSH mientras pruebas en otra terminal.
> Respaldo: la consola de VirtualBox sigue aceptando usuario y contraseña.
 
### 5.1 Conéctate a la VM por SSH
 
Desde tu PC, entra con la llave (te pedirá la passphrase):

```bash
ssh -i ~/.ssh/id_ed25519_vm node1@100.83.114.76
```
 
Todos los comandos siguientes se ejecutan **dentro de esa sesión SSH**, es
decir, ya dentro de la VM. No cierres esta sesión hasta terminar.
 
### 5.2 Crea el archivo de configuración
 
```bash
sudo nano /etc/ssh/sshd_config.d/00-no-password.conf
```
 
Contenido:
 
```
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
PermitRootLogin no
```
 
El prefijo `00-` hace que se lea primero y gane sobre otros archivos, como
`50-cloud-init.conf` (que suele traer `PasswordAuthentication yes`).
 
### 6.3 Valida y aplica
 
```bash
sudo sshd -t
sudo systemctl restart ssh
```
 
Reiniciar el servicio **no corta** la sesión SSH actual.
 
---
 
## 7. Verificar
 
### Configuración efectiva (en la VM)
 
```bash
sudo sshd -T | grep -Ei 'passwordauthentication|kbdinteractiveauthentication|pubkeyauthentication'
```
 
Resultado esperado:
 
```
passwordauthentication no
kbdinteractiveauthentication no
pubkeyauthentication yes
```
 
### Con contraseña, debe fallar (desde tu PC)
 
```bash
ssh -o PubkeyAuthentication=no node1@100.83.114.76
```
 
Resultado esperado:
 
```
node1@100.83.114.76: Permission denied (publickey).
```
 
---
 
## 8. Revertir
 
Desde la consola de VirtualBox (o por SSH con llave):
 
```bash
sudo rm /etc/ssh/sshd_config.d/00-no-password.conf
sudo systemctl restart ssh
```
 
---
 
## 9. Cerrar la conexión
 
```bash
exit        # o Ctrl+D
```
 
Si la sesión se congela: `Enter`, luego `~` y luego `.`
 
---
