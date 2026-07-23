#!/bin/bash
echo "Deploying to production..."
cd /var/www/app
git pull origin main
docker-compose down
docker-compose up -d
echo "Deployment completed."