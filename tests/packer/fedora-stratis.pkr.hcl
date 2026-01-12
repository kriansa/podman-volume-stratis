packer {
  required_plugins {
    qemu = {
      version = "~> 1"
      source  = "github.com/hashicorp/qemu"
    }
  }
}

variable "fedora_image_url" {
  type    = string
  default = "https://download.fedoraproject.org/pub/fedora/linux/releases/41/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-41-1.4.x86_64.qcow2"
}

variable "fedora_checksum_url" {
  type    = string
  default = "file:https://download.fedoraproject.org/pub/fedora/linux/releases/41/Cloud/x86_64/images/Fedora-Cloud-41-1.4-x86_64-CHECKSUM"
}

variable "headless" {
  type    = bool
  default = true
}

source "qemu" "fedora-stratis" {
  iso_url          = var.fedora_image_url
  iso_checksum     = var.fedora_checksum_url
  iso_target_path  = "isos"
  disk_image       = true
  output_directory = "../images"
  vm_name          = "fedora-stratis.qcow2"
  format           = "qcow2"
  disk_size        = "20G"

  ssh_username         = "fedora"
  ssh_password         = "fedora"
  ssh_timeout          = "10m"
  shutdown_command     = "sudo shutdown -P now"
  shutdown_timeout     = "5m"

  accelerator = "kvm"
  cpus        = 2
  cpu_model   = "host"
  memory      = 2048

  headless    = var.headless

  # Cloud-init configuration to set password
  cd_files = ["./http/meta-data", "./http/user-data"]
  cd_label = "cidata"
}

build {
  sources = ["source.qemu.fedora-stratis"]

  # Copy systemd service files
  provisioner "file" {
    source      = "scripts/stratis-test-setup.service"
    destination = "/tmp/stratis-test-setup.service"
  }

  provisioner "file" {
    source      = "scripts/podman-volume-stratis.service"
    destination = "/tmp/podman-volume-stratis.service"
  }

  provisioner "file" {
    source      = "scripts/move-plugin-binary.sh"
    destination = "/tmp/move-plugin-binary.sh"
  }

  provisioner "file" {
    source      = "scripts/stratisd-test-setup.conf"
    destination = "/tmp/stratisd-test-setup.conf"
  }

  provisioner "shell" {
    script = "scripts/setup-stratis.sh"
  }
}
