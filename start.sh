#!/bin/bash

# Start the WhatsApp MCP server
uv --directory whatsapp-mcp-server run main.py &
SERVER_PID=$!

# Start the WhatsApp Bridge
cd whatsapp-bridge
./whatsapp-bridge &
BRIDGE_PID=$!

# Wait for both processes
wait $SERVER_PID
wait $BRIDGE_PID 