#!/bin/bash

# Unilink Ubuntu 24.04 One-Click Deployment Script
# This script should be run on your Ubuntu server as root (sudo).

echo ">>> Starting Unilink Deployment on Ubuntu 24.04..."

# 1. Update and install MySQL if not exists
if ! command -v mysql &> /dev/null
then
    echo "[INFO] Installing MySQL Server..."
    sudo apt update
    sudo apt install -y mysql-server
fi

# 2. Setup Database
echo "[INFO] Setting up MySQL Database 'unilink'..."
sudo mysql -e "CREATE DATABASE IF NOT EXISTS unilink;"
# 创建包含 quota_bytes 的表，如果表已存在则尝试添加列
sudo mysql -e "USE unilink; CREATE TABLE IF NOT EXISTS user (id BIGINT NOT NULL AUTO_INCREMENT, username VARCHAR(255) NOT NULL UNIQUE, password VARCHAR(255) NOT NULL, storage_path VARCHAR(255) NOT NULL, quota_bytes BIGINT NOT NULL DEFAULT 104857600, PRIMARY KEY (id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;"
sudo mysql -e "USE unilink; IF NOT EXISTS (SELECT * FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='unilink' AND TABLE_NAME='user' AND COLUMN_NAME='quota_bytes') THEN ALTER TABLE user ADD COLUMN quota_bytes BIGINT NOT NULL DEFAULT 104857600; END IF;" || true

# Set root password if needed (Matching main.go: 43g3hqweg43q)
# Note: On Ubuntu 24.04, mysql uses auth_socket by default. 
# We'll set a password for the 'root' user to match your Go code.
sudo mysql -e "ALTER USER 'root'@'localhost' IDENTIFIED WITH mysql_native_password BY '43g3hqweg43q';"
sudo mysql -e "FLUSH PRIVILEGES;"

# 3. Create working directory
INSTALL_DIR="/opt/unilink"
sudo mkdir -p $INSTALL_DIR
sudo mkdir -p $INSTALL_DIR/storage
sudo cp ./unilink-server-linux $INSTALL_DIR/unilink-server
sudo chmod +x $INSTALL_DIR/unilink-server

# 4. Create Systemd Service
echo "[INFO] Creating Systemd Service..."
cat <<EOF | sudo tee /etc/systemd/system/unilink.service
[Unit]
Description=Unilink Go Backend (The Golden Soul)
After=network.target mysql.service

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/unilink-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 5. Firewall
echo "[INFO] Opening port 7890..."
if command -v ufw &> /dev/null
then
    sudo ufw allow 7890/tcp
fi

# 6. Start Service
echo "[INFO] Starting Unilink Service..."
sudo systemctl daemon-reload
sudo systemctl enable unilink
sudo systemctl restart unilink

echo ">>> DEPLOYMENT COMPLETE!"
echo ">>> Check status with: sudo systemctl status unilink"
echo ">>> View logs with: sudo journalctl -u unilink -f"
