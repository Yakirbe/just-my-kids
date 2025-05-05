#!/bin/bash

# Start the WhatsApp MCP server
cd whatsapp-mcp-server
python main.py &
SERVER_PID=$!

# Go back to root directory
cd ..

# Start the WhatsApp Bridge
cd whatsapp-bridge
./whatsapp-bridge &
BRIDGE_PID=$!

# Wait for both processes
wait $SERVER_PID
wait $BRIDGE_PID 