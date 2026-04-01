#!/bin/bash

# Function to check if a command exists
command_exists() {
  command -v "$1" >/dev/null 2>&1
}

# Function to check if a package is installed
package_exists() {
  dpkg -l "$1" 2>/dev/null | grep -q "^ii"
}

# Function to install a package using apt-get
install_apt_package() {
  if ! package_exists "$1"; then
    echo "Installing $1..."
    sudo apt-get update
    sudo apt-get install -y "$1"
    if package_exists "$1"; then
      echo "$1 installed successfully."
    else
      echo "Failed to install $1."
      exit 1
    fi
  else
    echo "$1 is already installed."
  fi
}

# Check and install Docker
if ! command_exists docker; then
  echo "Installing Docker..."
  curl -fsSL https://get.docker.com | sh
  if command_exists docker; then
    echo "Docker installed successfully."
  else
    echo "Failed to install Docker."
    exit 1
  fi
else
  echo "Docker is already installed."
fi

# Check and install Bun
if ! command_exists bun; then
  echo "Installing Bun..."
  curl -fsSL https://bun.sh/install | bash
  # Source the bash profile to update PATH
  if [ -f ~/.bashrc ]; then
    source ~/.bashrc
  fi
  if command_exists bun; then
    echo "Bun installed successfully."
  else
    echo "Failed to install Bun."
    exit 1
  fi
else
  echo "Bun is already installed."
fi

# Check and install Node.js and npm
if ! command_exists node; then
  echo "Installing Node.js..."
  sudo apt-get update
  sudo apt-get install -y nodejs npm
  if command_exists node; then
    echo "Node.js installed successfully."
  else
    echo "Failed to install Node.js."
    exit 1
  fi
else
  echo "Node.js is already installed."
fi

# Check and install Go
if ! command_exists go; then
  echo "Installing Go..."
  sudo apt-get update
  sudo apt-get install -y golang
  if command_exists go; then
    echo "Go installed successfully."
  else
    echo "Failed to install Go."
    exit 1
  fi
else
  echo "Go is already installed."
fi

# Check and install necessary Linux tools and libraries
install_apt_package ca-certificates
install_apt_package tzdata
install_apt_package libasan8
install_apt_package wget

echo "All necessary tools and libraries are installed."
