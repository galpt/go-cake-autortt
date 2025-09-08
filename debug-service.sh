#!/bin/sh
# Debug script for cake-autortt service issues

echo "=== cake-autortt Service Diagnostic Script ==="
echo "Timestamp: $(date)"
echo

# Check if binary exists and is executable
echo "1. Checking binary:"
PROG="/usr/bin/cake-autortt"
if [ -x "$PROG" ]; then
    echo "   ✓ Binary exists and is executable: $PROG"
    echo "   Binary info: $(ls -la $PROG)"
else
    echo "   ✗ Binary missing or not executable: $PROG"
fi
echo

# Check configuration file
echo "2. Checking configuration:"
CONF="/etc/cake-autortt.yaml"
if [ -f "$CONF" ]; then
    echo "   ✓ Config file exists: $CONF"
    echo "   Config file info: $(ls -la $CONF)"
    echo "   Config contents:"
    cat "$CONF" | sed 's/^/      /'
else
    echo "   ✗ Config file missing: $CONF"
fi
echo

# Check dependencies
echo "3. Checking dependencies:"
if command -v tc >/dev/null 2>&1; then
    echo "   ✓ tc (traffic control) is available"
    echo "   tc version: $(tc -V 2>&1 | head -1)"
else
    echo "   ✗ tc (traffic control) is missing"
fi
echo

# Check CAKE interfaces
echo "4. Checking CAKE interfaces:"
CAKE_INTERFACES=$(tc qdisc show | grep "qdisc cake" | awk '{print $5}' || true)
if [ -n "$CAKE_INTERFACES" ]; then
    echo "   ✓ CAKE interfaces found:"
    echo "$CAKE_INTERFACES" | sed 's/^/      /'
else
    echo "   ✗ No CAKE interfaces found"
    echo "   All qdiscs:"
    tc qdisc show | sed 's/^/      /'
fi
echo

# Test binary execution
echo "5. Testing binary execution:"
if [ -x "$PROG" ]; then
    echo "   Testing --help flag:"
    if "$PROG" --help >/dev/null 2>&1; then
        echo "   ✓ Binary responds to --help"
    else
        echo "   ✗ Binary failed --help test"
        echo "   Error output:"
        "$PROG" --help 2>&1 | sed 's/^/      /'
    fi
    
    echo "   Testing --version flag:"
    if "$PROG" --version >/dev/null 2>&1; then
        echo "   ✓ Binary responds to --version"
        echo "   Version: $($PROG --version 2>&1 | head -1)"
    else
        echo "   ✗ Binary failed --version test"
    fi
else
    echo "   ✗ Cannot test binary - not executable"
fi
echo

# Check service status
echo "6. Checking service status:"
if /etc/init.d/cake-autortt status >/dev/null 2>&1; then
    echo "   ✓ Service is running"
else
    echo "   ✗ Service is not running"
fi
echo

# Check recent logs
echo "7. Recent service logs:"
echo "   Last 10 cake-autortt log entries:"
logread | grep cake-autortt | tail -10 | sed 's/^/      /' || echo "      No logs found"
echo

# Test manual execution
echo "8. Testing manual execution (5 seconds):"
echo "   Starting manual test..."
if [ -x "$PROG" ] && [ -f "$CONF" ]; then
    timeout 5 "$PROG" --config "$CONF" 2>&1 | sed 's/^/      /' &
    MANUAL_PID=$!
    sleep 6
    if kill -0 $MANUAL_PID 2>/dev/null; then
        echo "   ✓ Manual execution successful (still running after 5s)"
        kill $MANUAL_PID 2>/dev/null
    else
        echo "   ✗ Manual execution failed or exited early"
    fi
else
    echo "   ✗ Cannot test manual execution - missing binary or config"
fi
echo

echo "=== Diagnostic Complete ==="
echo "If the service still fails to start, check:"
echo "1. System logs: logread | grep cake-autortt"
echo "2. Manual execution: $PROG --config $CONF"
echo "3. Permissions: ls -la $PROG $CONF"
echo "4. Dependencies: ldd $PROG"