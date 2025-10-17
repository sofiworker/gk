package rawcap

import (
	"fmt"
	"log"
	"net"
	"syscall"
)

// 手动实现 htons（Host to Network Short）
func htons(i uint16) uint16 {
	return (i << 8) | (i >> 8)
}

func main() {
	// syscall.ETH_P_ALL
	const ETH_P_ALL = 0x0003
	protocol := int(htons(ETH_P_ALL))

	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, protocol)
	if err != nil {
		log.Fatal("Socket error:", err)
	}
	defer syscall.Close(fd)
	fmt.Printf("Socket created: fd=%d\n", fd)

	iface, err := net.InterfaceByName("ens33")
	if err != nil {
		log.Fatal("InterfaceByName error:", err)
	}
	fmt.Printf("Interface found: %s, Index=%d\n", iface.Name, iface.Index)

	addr := syscall.SockaddrLinklayer{
		Protocol: uint16(htons(ETH_P_ALL)),
		Ifindex:  iface.Index,
	}

	if err := syscall.Bind(fd, &addr); err != nil {
		log.Fatal("Bind error:", err)
	}
	fmt.Println("Socket bound.")

	buf := make([]byte, 65535)
	fmt.Println("Listening...")

	for {
		n, from, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			log.Printf("Recvfrom error: %v", err)
			continue
		}
		fmt.Printf("✅ Go: Received %d bytes from %v: % x\n", n, from, buf[:n])
	}
}

//const ETH_P_ALL = 0x0003
//
//func main() {
//	fmt.Println("Creating socket...")
//	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(ETH_P_ALL)))
//	if err != nil {
//		log.Fatal("Socket error:", err)
//	}
//	defer unix.Close(fd)
//	fmt.Printf("Socket created: fd=%d\n", fd)
//
//	iface, err := net.InterfaceByName("ens33")
//	if err != nil {
//		log.Fatal("InterfaceByName error:", err)
//	}
//	fmt.Printf("Interface: %s, Index=%d\n", iface.Name, iface.Index)
//
//	addr := &unix.SockaddrLinklayer{
//		Protocol: unix.ETH_P_ALL,
//		Ifindex:  iface.Index,
//	}
//
//	fmt.Printf("Binding to ifindex %d, protocol %d (0x%04x)...\n", addr.Ifindex, addr.Protocol, addr.Protocol)
//	if err := unix.Bind(fd, addr); err != nil {
//		log.Fatal("Bind error:", err)
//	}
//	fmt.Println("✅ Bind successful.")
//
//	buf := make([]byte, 65535)
//	fmt.Println("Receiving...")
//
//	for {
//		n, from, err := unix.Recvfrom(fd, buf, 0)
//		if err != nil {
//			log.Printf("Recvfrom error: %v", err)
//			continue
//		}
//		if n == 0 {
//			continue
//		}
//		fmt.Printf("✅ RECEIVED %d bytes from %v: % x\n", n, from, buf[:n])
//		break
//	}
//}
