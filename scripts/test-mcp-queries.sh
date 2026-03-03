#!/bin/bash
# Test the same MCP queries the other agent ran against lazydb
# Requires: atlaskb serve running on port 3000

BASE="http://localhost:3000/mcp"
HEADERS='-H "Content-Type: application/json" -H "Accept: application/json, text/event-stream"'

# Step 1: Initialize session and capture Mcp-Session-Id
echo "=== Initializing MCP session ==="
INIT_RESP=$(curl -s -i -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}')

SESSION_ID=$(echo "$INIT_RESP" | grep -i "mcp-session-id" | sed 's/.*: //' | tr -d '\r')
echo "Session ID: $SESSION_ID"
echo ""

call_tool() {
  local id=$1
  local name=$2
  local args=$3
  local label=$4

  echo "=== $label ==="
  curl -s -X POST "$BASE" \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -H "Mcp-Session-Id: $SESSION_ID" \
    -d "{\"jsonrpc\":\"2.0\",\"id\":$id,\"method\":\"tools/call\",\"params\":{\"name\":\"$name\",\"arguments\":$args}}" \
    | grep -o '"result":{[^}]*"text":"[^"]*' | head -1
  echo ""
  echo ""
}

# Query 1: get_task_context
echo "=== Query 1: get_task_context ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_task_context","arguments":{"repo":"lazydb","files":["src/db/sql_builder.rs","src/db/mod.rs","src/db/mysql.rs","src/types.rs","src/app.rs","src/app/input_handlers.rs"],"depth":"deep"}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 2000
echo ""
echo ""

# Query 2: get_module_context - src/db/mysql.rs
echo "=== Query 2: get_module_context src/db/mysql.rs ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_module_context","arguments":{"repo":"lazydb","path":"src/db/mysql.rs","depth":"deep"}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 2000
echo ""
echo ""

# Query 3: get_module_context - src/types.rs
echo "=== Query 3: get_module_context src/types.rs ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_module_context","arguments":{"repo":"lazydb","path":"src/types.rs","depth":"deep"}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 2000
echo ""
echo ""

# Query 4: get_service_contract - src/db/mod.rs
echo "=== Query 4: get_service_contract src/db/mod.rs ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_service_contract","arguments":{"repo":"lazydb","path":"src/db/mod.rs"}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 2000
echo ""
echo ""

# Query 5: get_impact_analysis - src/app.rs
echo "=== Query 5: get_impact_analysis src/app.rs ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_impact_analysis","arguments":{"repo":"lazydb","path":"src/app.rs","max_hops":3}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 2000
echo ""
echo ""

# Query 6: search - DatabaseOperations trait
echo "=== Query 6: search 'DatabaseOperations trait mysql connection query' ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"DatabaseOperations trait mysql connection query","repo":"lazydb","limit":20}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 3000
echo ""
echo ""

# Query 7: search - SqlBuilder
echo "=== Query 7: search 'SqlBuilder struct query building methods' ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"SqlBuilder struct query building methods","repo":"lazydb","limit":20}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 3000
echo ""
echo ""

# Query 8: search - App struct TUI
echo "=== Query 8: search 'App struct state management TUI input handling' ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"App struct state management TUI input handling","repo":"lazydb","limit":20}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 3000
echo ""
echo ""

# Query 9: search - keyboard input
echo "=== Query 9: search 'keyboard input handler key event focus view mode' ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"keyboard input handler key event focus view mode","repo":"lazydb","limit":20}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 3000
echo ""
echo ""

# Query 10: search - sql_builder quote
echo "=== Query 10: search 'sql_builder quote identifier build query' ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"sql_builder quote identifier build query","repo":"lazydb","limit":10}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 3000
echo ""
echo ""

# Query 11: search - mysql execute
echo "=== Query 11: search 'mysql execute query row fetch result' ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"mysql execute query row fetch result","repo":"lazydb","limit":10}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 3000
echo ""
echo ""

# Query 12: search - error handling (graph mode)
echo "=== Query 12: search 'error handling Result anyhow' mode=graph ==="
RESP=$(curl -s -X POST "$BASE" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"error handling Result anyhow","repo":"lazydb","limit":10,"mode":"graph"}}}')
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP" | head -c 3000
echo ""

echo "=== DONE ==="
