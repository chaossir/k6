essentials:
  pkg.installed:
    - pkgs:
      - build-essential
      - vim
      - git
      - tmux
      - htop
      - strace
      - ltrace
      - zsh
      - curl

apt-transport-https:
  pkg.installed

vm.swappiness:
  sysctl.present:
    - value: 0

fs.file-max:
  sysctl.present:
    - value: 65535