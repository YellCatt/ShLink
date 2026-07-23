#!/bin/bash
echo "Running tests on staging..."
cd /var/www/app
npm test
echo "Tests completed."