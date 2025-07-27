#!/bin/bash

set -e

# Create the directory if it doesn't exist
mkdir -p /opt/render/.auto-gmail

# Copy the raw token file (no base64 decoding)
cp ./assets/token.base64.txt /opt/render/.auto-gmail/token.json

# Now start the Gmail MCP server
./gmail-mcp-server --http
