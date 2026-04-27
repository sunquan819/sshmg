#!/bin/bash

if ! command -v docker &> /dev/null
then
    echo "Docker is not installed, starting installation..."
    # Commands to install Docker
else
    echo "Docker is already installed, no need to reinstall."
    exit 0
fi

# Variables for paths and versions
DOCKER_VERSION="28.2.2"
DOCKER_URL="https://download.docker.com/linux/static/stable/x86_64/docker-${DOCKER_VERSION}.tgz"
DOCKER_INSTALL_PATH="/usr/bin"
DOCKER_SERVICE_PATH="/etc/systemd/system"
DOCKER_CONFIG_PATH="/etc/docker"

# Download and set up Docker
if [ ! -f "docker-${DOCKER_VERSION}.tgz" ]; then
    wget "${DOCKER_URL}" -O "docker-${DOCKER_VERSION}.tgz"
fi

tar xf "docker-${DOCKER_VERSION}.tgz"
chown root:root docker/*
cp -p docker/* "${DOCKER_INSTALL_PATH}/"

# Create Docker group and user if they don't exist
if ! getent group docker > /dev/null; then
    groupadd docker
fi

if ! id docker > /dev/null 2>&1; then
    useradd -g docker docker
fi

# Configure Docker
mkdir -p "${DOCKER_CONFIG_PATH}"
cat > "${DOCKER_CONFIG_PATH}/daemon.json" << EOF
{
  "hosts": ["unix:///var/run/docker.sock"],
  "live-restore": true,
  "log-driver": "json-file",
  "log-opts": {"max-size":"100m", "max-file":"3"},
  "data-root":"/workdir/docker/",
  "insecure-registries" : ["reg.local.seatone.com"],
  "bip": "192.166.39.1/24"
}
EOF

# Create and configure Docker service file
cat > "${DOCKER_SERVICE_PATH}/docker.service" << EOF
[Unit]
Description=Docker Application Container Engine
Documentation=https://docs.docker.com
After=network-online.target firewalld.service
Wants=network-online.target

[Service]
Type=notify
ExecStart=/usr/bin/dockerd 
ExecReload=/bin/kill -s HUP $MAINPID
LimitNOFILE=infinity
LimitNPROC=infinity
LimitCORE=infinity
TimeoutStartSec=0
Delegate=yes
KillMode=process
Restart=on-failure
StartLimitBurst=3
StartLimitInterval=60s

[Install]
WantedBy=multi-user.target
EOF

# Create and configure Docker socket file
cat > "${DOCKER_SERVICE_PATH}/docker.socket" << EOF
[Unit]
Description=Docker Socket for the API
PartOf=docker.service

[Socket]
ListenStream=/var/run/docker.sock
SocketMode=0660
SocketUser=root
SocketGroup=docker

[Install]
WantedBy=sockets.target
EOF

# Create and configure Containerd service file
cat > "${DOCKER_SERVICE_PATH}/containerd.service" << EOF
[Unit]
Description=containerd container runtime
Documentation=https://containerd.io
After=network.target local-fs.target

[Service]
ExecStartPre=-/sbin/modprobe overlay
ExecStart=/usr/bin/containerd
Type=notify
Delegate=yes
KillMode=process
Restart=always
RestartSec=5
LimitNPROC=infinity
LimitCORE=infinity
LimitNOFILE=infinity
TasksMax=infinity
OOMScoreAdjust=-999

[Install]
WantedBy=multi-user.target
EOF

# Set the appropriate permissions for service files
chmod a+x "${DOCKER_SERVICE_PATH}/docker.service"
chmod a+x "${DOCKER_SERVICE_PATH}/docker.socket"
chmod a+x "${DOCKER_SERVICE_PATH}/containerd.service"

# Reload systemd to recognize new services, enable, and start them
systemctl daemon-reload
systemctl enable containerd.service
systemctl start containerd.service
systemctl enable docker.service
systemctl start docker.service

# Install docker-compose
#if [ ! -f "docker-compose-linux-x86_64" ]; then
 #   curl -SL https://github.com/docker/compose/releases/download/v2.14.0/docker-compose-linux-x86_64 -o /usr/local/bin/docker-compose   
 #   chmod 755 /usr/local/bin/docker-compose
#else
mv docker-compose-linux-x86_64  /usr/bin/docker-compose
chmod 755 /usr/bin/docker-compose
#fi
