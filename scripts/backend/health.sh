#!/bin/bash
echo "Checking backend health..."
curl -s http://localhost:8080/health
echo ""
echo "Health check completed."