# Packer golden-image build for the Fleet appliance (Hyper-V).
#
# Bakes the EXPENSIVE work once — OS + Docker + Go/Node + cloned fork + built Fleet + pulled MySQL/Redis
# images — then GENERALIZES the image so each clone personalizes only the cheap per-instance bits (unique
# server private key, fresh DB, migrations) on first boot. Result: clones boot to a working Fleet in
# ~1 min instead of the ~15 min from-scratch build.
#
# Build:  packer init . ; packer build -var repo_url=https://github.com/your-org/fleet.git .
# Output: output-fleet-golden/*.vhdx  -> import/clone with New-VM -VHDPath, or convert to a Hyper-V template.
#
# NOTE: untested in this environment (no Hyper-V/Packer here). Treat as a working skeleton to iterate on the
# VM — boot_command menu timing and iso_checksum in particular may need tuning for your Rocky ISO.

packer {
  required_plugins {
    hyperv = {
      source  = "github.com/hashicorp/hyperv"
      version = ">= 1.1.0"
    }
  }
}

variable "iso_url"      { type = string  default = "C:/isos/Rocky-9-latest-x86_64-minimal.iso" }
variable "iso_checksum" { type = string  default = "none" } # e.g. "file:https://.../CHECKSUM" — set for real builds
variable "repo_url"     { type = string }                    # required; use https://<token>@... for a private fork
variable "branch"       { type = string  default = "human-dev" }
variable "switch_name"  { type = string  default = "Default Switch" }

source "hyperv-iso" "fleet" {
  iso_url      = var.iso_url
  iso_checksum = var.iso_checksum
  generation   = 2
  cpus         = 4
  memory       = 4096
  disk_size    = 61440

  switch_name          = var.switch_name
  secure_boot_enabled  = true
  secure_boot_template = "MicrosoftUEFICertificateAuthority"

  # Kickstart delivered on an OEMDRV-labeled CD (Anaconda auto-loads /ks.cfg) — same trick as the one-off
  # appliance, no boot-param editing. cd_files preserves the basename, so the file must be named ks.cfg.
  cd_files = ["./ks.cfg"]
  cd_label = "OEMDRV"

  # After the unattended install reboots, Packer connects as the packer user the kickstart creates.
  communicator     = "ssh"
  ssh_username     = "packer"
  ssh_password     = "packer"
  ssh_timeout      = "45m"
  shutdown_command = "sudo shutdown -P now"

  # The default Rocky 9 boot menu highlights "Test this media & install" (slow mediacheck). Nudge to the
  # "Install Rocky Linux 9" entry. Tune if your ISO's menu differs.
  boot_wait    = "5s"
  boot_command = ["<up><enter>"]

  output_directory = "output-fleet-golden"
}

build {
  sources = ["source.hyperv-iso.fleet"]

  # 1. Bake: install everything + build Fleet by running our own Ansible playbook against localhost.
  provisioner "shell" {
    environment_vars = ["REPO_URL=${var.repo_url}", "BRANCH=${var.branch}"]
    execute_command  = "sudo -E bash '{{ .Path }}'"
    script           = "scripts/provision.sh"
  }

  # 2. Generalize: strip per-instance state and install the first-boot personalize service.
  provisioner "shell" {
    execute_command = "sudo -E bash '{{ .Path }}'"
    script          = "scripts/generalize.sh"
  }
}
