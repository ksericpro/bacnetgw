Yes, it is entirely possible and a common pattern in building automation and IoT.

BACnet/IP operates primarily over **UDP on port 47808** (`0xBAC0`). Because standard BACnet discovery uses broadcast UDP packets (`Who-Is` and `I-Am`), an application running on a machine in the same local subnet/broadcast domain can bind to the port and passively monitor or actively query network traffic.

---

### Key Networking Constraints to Keep in Mind

1. **Broadcast Domain (Subnetting):**
Standard BACnet discovery packets (`Who-Is` / `I-Am`) are broadcast locally (`255.255.255.255` or subnet-directed broadcast like `192.168.1.255`). If your listener is on the same VLAN/subnet as the BACnet devices, it will capture these natively. If devices live across routed subnets, you will need a **BBMD** (BACnet Broadcast Management Device) and register your listener as a Foreign Device.
2. **Port Binding Conflicts:**
If you run your listener on a machine that is already running BACnet software (like a Niagara service or another BMS client bound to `0.0.0.0:47808`), your app won't be able to bind exclusively to port 47808 without setting the `SO_REUSEPORT` socket option.
3. **Unicast vs. Broadcast:**
* **Broadcast traffic:** Passively reading port 47808 gives you `Who-Is`, `I-Am`, and unconfirmed change-of-value (COV) notifications.
* **Unicast traffic:** Standard point-to-point traffic (like a BMS reading an analog input via `ReadProperty`) goes directly between the controller's IP and the workstation's IP. A standard UDP socket listener will **not** see other devices' unicast traffic unless you are:
* Tapping the switch (SPAN/mirror port),
* Running on a hub (rare), or
* Actively querying devices yourself rather than purely eavesdropping.





---

### Recommended Go Libraries

Instead of writing a raw BVLC (BACnet Virtual Link Layer Control) and NPDU parser from scratch, consider these open-source Go implementations:

* **[`github.com/grid-x/gobacnet`](https://www.google.com/search?q=https://github.com/grid-x/gobacnet):** A native Go BACnet/IP stack. Good for actively creating a virtual client, sending `Who-Is`, and reading/writing properties.
* **[`github.com/google/gopacket`](https://github.com/google/gopacket):** If you want a passive network sniffer (promiscuous mode/pcap) that captures all BACnet packets passing through an interface, `gopacket` has native support for BACnet decoding (layers for BVLC and NPDU/APDU).

---

### Minimal Go UDP Listener Example

If you want to listen for local broadcast announcements (like `I-Am` and `Who-Is`) without external dependencies:

```go
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"
)

const (
	bacnetPort = 47808 // 0xBAC0
	bufferSize = 2048
)

func main() {
	// Bind to all interfaces on UDP port 47808
	addr := net.UDPAddr{
		Port: bacnetPort,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp4", &addr)
	if err != nil {
		log.Fatalf("Failed to bind UDP port %d: %v", bacnetPort, err)
	}
	defer conn.Close()

	fmt.Printf("BACnet listener active on 0.0.0.0:%d...\n", bacnetPort)

	buf := make([]byte, bufferSize)

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("Error reading packet: %v", err)
			continue
		}

		packet := buf[:n]

		// BACnet/IP packets start with BVLC Type 0x81
		if len(packet) > 0 && packet[0] == 0x81 {
			bvlcFunction := packet[1]
			fmt.Printf("\n[Received %d bytes from %s]\n", n, remoteAddr)
			fmt.Printf("BVLC Function: 0x%02X\n", bvlcFunction)
			fmt.Printf("Raw Payload: %s\n", hex.EncodeToString(packet))
		}
	}
}

```

### Packet Structure Cheat Sheet

When parsing raw packets, the header layers break down as follows:

| Layer | Size | Purpose | Key Signatures |
| --- | --- | --- | --- |
| **BVLC** | 4 bytes | BACnet/IP Framing | Byte 0: `0x81` (Type)<br>

<br>Byte 1: `0x0A` (Original-Unicast) or `0x0B` (Original-Broadcast)<br>

<br>Bytes 2–3: Length |
| **NPDU** | 2–10+ bytes | Network Layer | Controls routing, hop counts, and source/destination network mapping |
| **APDU** | Variable | Application Data | Contains service choices (`Who-Is`, `I-Am`, `ReadProperty`, `COVNotification`) |