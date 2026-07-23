#!/bin/bash
echo "Running system update..."
sudo apt-get update && sudo apt-get upgrade -y
echo "System update completed."