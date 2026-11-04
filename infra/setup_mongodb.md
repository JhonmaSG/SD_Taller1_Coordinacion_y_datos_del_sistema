# Instalar y configurar MongoDB 
Se deberá seguir los siguientes pasos para cada uno de los nodos a tener.

### Aclaraciones
Al usar Ubuntu Server 26.04.1 LTS se tiene un conflicto con las versiones de MongoDB 8.x o inferiores, esto se debe a que MongoDB realizar una validación de compatibilidad del kernel de linux a la hora de iniciar el motor de mongoDB. Esto nos obligó a instalar MongoDB 9 para solventar este problema

## 1. Pasos a seguir
1. Instalar dependencias
```bash
sudo apt update
sudo apt install -y gnupg curl
``` 

2. Importar la llave GPG oficial
```bash
curl -fsSL https://pgp.mongodb.com/server-9.asc | \
sudo gpg -o /usr/share/keyrings/mongodb-server-9.gpg \
--dearmor
```

3. Crear el repositorio para Ubuntu 26.04
```bash
echo "deb [ arch=amd64,arm64 signed-by=/usr/share/keyrings/mongodb-server-9.gpg ] https://repo.mongodb.org/apt/ubuntu resolute/mongodb-org/9.0 multiverse" | \
sudo tee /etc/apt/sources.list.d/mongodb-org-9.0.list
```

4. Actualizar repositorios
```bash
sudo apt update
```

5. Comprobar versión disponible
```bash
apt-cache policy mongodb-org
```

6. Validar que en el campo `Candidate` sea 9.x
```bash
 Candidate: 9.0.4
```

7. Instalar MongoDB 9.x
```bash
sudo apt install -y mongodb-org
```

8. Iniciar MongoDB
```bash
sudo systemctl enable mongod
sudo systemctl start mongod
```

9. Comprobar que MongoDB esté corriendo con el comando `systemctl status mongod` por lo que deberá mostrar lo siguiente:
```bash
Active: active (running) ...
```
