#!/bin/bash
set -xeuo pipefail

echo "Installing Stratis..."
sudo dnf install -y stratis-cli stratisd

echo "Installing service units..."
sudo cp /tmp/stratis-test-setup.service /etc/systemd/system/
sudo cp /tmp/podman-volume-stratis.service /etc/systemd/system/
sudo mkdir -p /etc/systemd/system/stratisd.service.d
sudo cp /tmp/stratisd-test-setup.conf /etc/systemd/system/stratisd.service.d/
sudo systemctl daemon-reload
sudo systemctl enable stratis-test-setup

echo "Installing move-plugin-binary.sh..."
sudo mv /tmp/move-plugin-binary.sh /usr/local/bin/move-plugin-binary.sh
sudo chown root:root /usr/local/bin/move-plugin-binary.sh
sudo chmod 755 /usr/local/bin/move-plugin-binary.sh
sudo restorecon -v /usr/local/bin/move-plugin-binary.sh

echo "Starting stratisd..."
sudo systemctl enable --now stratisd

echo "Creating Stratis pool 'test_pool'..."
sudo stratis pool create test_pool /dev/loop0

echo "Verifying pool creation..."
sudo stratis pool list

echo "Ensuring SSH is enabled..."
sudo systemctl enable sshd

echo "Stratis setup complete"
sudo stratis pool list

echo "Cleaning up for a smaller image"
sudo dnf clean all
sudo rm -rf /var/cache/dnf/*

echo "Removing cloud-init (no longer needed after image build)..."
sudo cloud-init clean --logs
sudo dnf remove -y cloud-init
sudo rm -rf /etc/cloud /var/lib/cloud
