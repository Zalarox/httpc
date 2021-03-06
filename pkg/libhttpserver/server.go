package libhttpserver

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func readRequestFromConnection(conn net.Conn) ([]byte, error) {
	temp := make([]byte, buffSize)
	data := make([]byte, 0)
	length := 0

	for {
		n, err := conn.Read(temp)

		if err != nil {
			break
		}

		data = append(data, temp[:n]...)
		length += n
		if n < buffSize {
			break
		}
	}

	return data, nil
}

func LogInfo(logString string) {
	if verboseLogging {
		log.Println(logString)
	}
}

func findRoute(parsedRequest *Request) (handlerFn, string) {
	paths := strings.Split(parsedRequest.route, "/")
	if len(paths) > 2 {
		return routeMap[parsedRequest.Method]["/"], parsedRequest.route
	}
	return routeMap[parsedRequest.Method]["/"], paths[len(paths)-1]
}

func parsePacket(data []byte) UDPPacket {
	pType := data[0]
	seqNo := data[1:5]
	peerAddr := data[5:9]
	peerPort := data[9:11]
	payload := data[11:]

	return UDPPacket{
		pType:    []byte{pType},
		seqNo:    seqNo,
		peerAddr: peerAddr,
		peerPort: peerPort,
		payload:  payload,
	}
}

func getBytesFromPacket(packet UDPPacket) []byte {
	packetBytes := append(packet.pType, packet.seqNo...)
	packetBytes = append(packetBytes, packet.peerAddr...)
	packetBytes = append(packetBytes, packet.peerPort...)
	packetBytes = append(packetBytes, packet.payload...)
	return packetBytes
}

func MakePacket(pType uint32, seqNo uint32, addr string, port uint16, payload string) UDPPacket {

	// pType, one of the following: 0 - Data, 1- ACK, 2 - SYN, 3 - SYN-ACK, 4 - NAK; 1 byte
	pTypeByte := []byte{byte(pType)}

	// seqNo, for SYN it is the initial pNo during 3WH -- else incremental packet numbers; 4 bytes BE
	seqNoBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(seqNoBytes, seqNo)

	// peerAddr, either sender/receiver -- translated by router!; 4 bytes
	peerAddrBytes := make([]byte, 4)

	peerAddrSplit := strings.Split(addr, ".")
	for i, section := range peerAddrSplit {
		intSection, _ := strconv.Atoi(section)
		peerAddrBytes[i] = byte(intSection)
	}

	// peerPort, either sender/receiver -- translated by router!; 2 bytes BE
	peerPortBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(peerPortBytes, port)

	// payload; max 1013 bytes
	// TODO handle size constraints/breaking somehow...
	payloadBytes := []byte(payload)

	// Packet Size Range: 11 (no payload) to 1024 (full payload)
	return UDPPacket{
		pType:    pTypeByte,
		seqNo:    seqNoBytes,
		peerAddr: peerAddrBytes,
		peerPort: peerPortBytes,
		payload:  payloadBytes,
	}
}

func inNaks(seqNo uint32, naks []uint32) bool {
	for _, nakSeq := range naks {
		if nakSeq == seqNo {
			return true
		}
	}
	return false
}

func inAcks(seqNo uint32, acks []uint32) bool {
	for _, ackSeq := range acks {
		if ackSeq == seqNo {
			return true
		}
	}
	return false
}

func getAddressFromBytes(packet UDPPacket) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		packet.peerAddr[0], packet.peerAddr[1], packet.peerAddr[2], packet.peerAddr[3])
}

func getPortFromBytes(packet UDPPacket) int {
	return int(binary.BigEndian.Uint16(packet.peerPort))
}

func handleConnection(curConn net.Conn) {
	LogInfo(fmt.Sprintf("Handling client %s", curConn.RemoteAddr().String()))
	defer curConn.Close()

	requestData, err := readRequestFromConnection(curConn)
	var response string
	var statusCode int
	var headers string

	if err != nil {
		LogInfo("Read request error!")
	}

	parsedRequest := parseRequestData(string(requestData))
	handler := routeMap[parsedRequest.Method][parsedRequest.route]

	if handler != nil {
		response, statusCode, headers = handler(parsedRequest, nil, &rootDirectory)
	} else {
		handler, pathParam := findRoute(parsedRequest)
		response, statusCode, headers = handler(parsedRequest, &pathParam, &rootDirectory)
	}

	httpResponse := constructStructuredResponse(response, statusCode, headers)
	_, writeErr := curConn.Write([]byte(httpResponse))
	if writeErr != nil {
		LogInfo("Connection write error!")
	}
	LogInfo(fmt.Sprintf("Responded to %s with status code %d", curConn.RemoteAddr().String(), statusCode))
}

func handleUdpConnection(requestPayload string) *string {
	var response string
	var statusCode int
	var headers string

	parsedRequest := parseRequestData(requestPayload)
	handler := routeMap[parsedRequest.Method][parsedRequest.route]

	if handler != nil {
		response, statusCode, headers = handler(parsedRequest, nil, &rootDirectory)
	} else {
		handler, pathParam := findRoute(parsedRequest)
		response, statusCode, headers = handler(parsedRequest, &pathParam, &rootDirectory)
	}

	httpResponse := constructStructuredResponse(response, statusCode, headers)

	return &httpResponse
}

func constructStructuredResponse(response string, statusCode int, headers string) string {
	statusLine := fmt.Sprintf("HTTP/1.0 %d %s %s", statusCode, reasonPhrase[statusCode], CRLF)
	return fmt.Sprintf("%s%s%s%s", statusLine, headers, CRLF+CRLF, response)
}

func parseRequestData(request string) *Request {
	initialSplit := strings.SplitN(request, CRLF+CRLF, 2)
	requestLines := strings.Split(initialSplit[0], CRLF)
	cleanedRequestLines := []string{}
	parsedRequest := Request{}

	for _, line := range requestLines {
		if line != blankString {
			cleanedRequestLines = append(cleanedRequestLines, line)
		}
	}

	firstReqLine := strings.Split(cleanedRequestLines[0], " ")
	parsedRequest.route = firstReqLine[1]

	if strings.Contains(cleanedRequestLines[0], "POST") {
		parsedRequest.Method = "POST"
		headers := strings.Join(cleanedRequestLines[1:len(cleanedRequestLines)], CRLF)
		parsedRequest.headers = &headers
		parsedRequest.Body = &initialSplit[1]
	} else {
		parsedRequest.Method = "GET"
		if len(cleanedRequestLines) > 1 {
			headers := strings.Join(cleanedRequestLines[1:len(cleanedRequestLines)-1], CRLF)
			parsedRequest.headers = &headers
		}
	}

	return &parsedRequest
}

func RegisterHandler(method string, route string, handler handlerFn) {
	if routeMap[method] == nil {
		routeMap[method] = map[string]handlerFn{}
	}
	routeMap[method][route] = handler
}

func StartUDPServer(port string, directory string, verbose bool) {
	portInt, _ := strconv.Atoi(port)
	serverIP, _, _ := net.ParseCIDR("127.0.0.1/8")
	serverAddr := net.UDPAddr{
		IP:   serverIP,
		Port: portInt,
	}
	udpConn, err := net.ListenUDP("udp", &serverAddr)

	if _, err := os.Stat(directory); os.IsNotExist(err) {
		log.Fatal("Directory not found.")
	}

	rootDirectory = directory
	verboseLogging = verbose

	if err != nil {
		fmt.Println(err)
		return
	}

	defer udpConn.Close()

	clients := new(sync.Map)
	doneMap := new(sync.Map)
	for {
		buffer := make([]byte, 1024)
		udpConn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := udpConn.ReadFromUDP(buffer)

		if err != nil {
			continue
		}

		packet := parsePacket(buffer[:n])
		hostAddr := getAddressFromBytes(packet)
		hostPort := getPortFromBytes(packet)
		clientKey := fmt.Sprintf("%s:%d", hostAddr, hostPort)
		clientPackets, loaded := clients.LoadOrStore(clientKey, make(chan UDPPacket, 1024))
		clientDone, _ := doneMap.LoadOrStore(clientKey, make(chan bool, 1))

		if !loaded {
			go func() {
				var expectedSeqNo uint32
				expectedSeqNo = 4
				acks := make([]uint32, 5)
				naks := make([]uint32, 5)
				var responseNaksList []UDPPacket
				httpPayload := make([]string, 1024)
				var totalNumPackets int // might need to set this to a large number
				var responsePackets []UDPPacket
				//var numOfResponsePackets int

				for packet := range clientPackets.(chan UDPPacket) {
					timeout := 2 * time.Second
					deadline := time.Now().Add(timeout)
					_ = udpConn.SetWriteDeadline(deadline)
					receivedSeqNo := binary.BigEndian.Uint32(packet.seqNo)

					if packet.pType[0] == 4 {
						responseNaksList = append(responseNaksList, packet)
						sendUnreceivedResponsePackets(responseNaksList, responsePackets, udpConn, addr)
					} else if packet.pType[0] == 1 {
						if receivedSeqNo == 3 {
							continue
						}
						responseNaksList = remove(responseNaksList, packet)
						sendUnreceivedResponsePackets(responseNaksList, responsePackets, udpConn, addr)
					}

					if packet.pType[0] == 0 { // add an && for if totalNumPackets is not known after a timeout then close
						if inAcks(receivedSeqNo, acks) {
							continue
						}
						acks = append(acks, receivedSeqNo)
						if receivedSeqNo == expectedSeqNo {
							// SEND ACK
							ackPacket := MakePacket(1, receivedSeqNo, hostAddr, binary.BigEndian.Uint16(packet.peerPort), "")
							packetBytes := getBytesFromPacket(ackPacket)
							_, writeErr := udpConn.WriteToUDP(packetBytes, addr)
							if writeErr != nil {
								LogInfo("Timeout packet 0!")
							}
							// STORE payload in proper structure
							httpPayload[int(receivedSeqNo)] = string(packet.payload)
							LogInfo(fmt.Sprintf("ACK'd packet %d", receivedSeqNo))
							expectedSeqNo += 1
						} else if receivedSeqNo < expectedSeqNo {
							// retransmitted packet from client
							// SEND ACK
							ackPacket := MakePacket(1, receivedSeqNo, hostAddr, binary.BigEndian.Uint16(packet.peerPort), "")
							packetBytes := getBytesFromPacket(ackPacket)
							_, writeErr := udpConn.WriteToUDP(packetBytes, addr)
							if writeErr != nil {
								LogInfo("Timeout for retransmitted!")
							}
							LogInfo(fmt.Sprintf("ACK'd packet %d", receivedSeqNo))
							httpPayload[int(receivedSeqNo)] = string(packet.payload)
						} else {
							// SEND ACK
							ackPacket := MakePacket(1, receivedSeqNo, hostAddr, binary.BigEndian.Uint16(packet.peerPort), "")
							packetBytes := getBytesFromPacket(ackPacket)
							_, writeErr := udpConn.WriteToUDP(packetBytes, addr)
							if writeErr != nil {
								LogInfo("Timeout for higher seqNo!")
							}
							LogInfo(fmt.Sprintf("ACK'd packet %d", receivedSeqNo))
							httpPayload[int(receivedSeqNo)] = string(packet.payload)
							for packetNum := expectedSeqNo; packetNum < receivedSeqNo; packetNum++ {
								naks = append(naks, packetNum)
								nakPacket := MakePacket(4, packetNum, hostAddr, binary.BigEndian.Uint16(packet.peerPort), "")
								packetBytes := getBytesFromPacket(nakPacket)
								_, writeErr := udpConn.WriteToUDP(packetBytes, addr)
								if writeErr != nil {
									LogInfo("Timeout writing NAKs!")
								}
								LogInfo(fmt.Sprintf("NAK'd packet %d", packetNum))
							}
							expectedSeqNo = receivedSeqNo + 1
						}
						// check if we are done reading the payload
						if totalNumPackets == 1 && len(httpPayload[4]) > 0 {
							// single packet request payload
							responsePackets, _ = writeResponseToClient(httpPayload, totalNumPackets, hostAddr, hostPort, udpConn, addr)
						} else {
							// single packet request payload
							if checkNotEmpty(httpPayload[4:(4 + totalNumPackets)]) {
								responsePackets, _ = writeResponseToClient(httpPayload, totalNumPackets, hostAddr, hostPort, udpConn, addr)
							}
						}
					}
					handshakePayload := handleHandshakePacket(packet, addr, udpConn)
					if handshakePayload != nil && *handshakePayload > 0 {
						totalNumPackets = *handshakePayload
					}
				}
			}()
		}

		select {
		case done := <-clientDone.(chan bool):
			// Really bad way but this ensures no sending to closed channel
			// release resources only when you detect a stale retransmission
			if done {
				timeOut(clients, clientKey)
			}
		default:
			select {
			case clientPackets.(chan UDPPacket) <- packet:
			default:
				fmt.Println("Failed to buffer packet!!")
			}
		}
	}
}

func sendUnreceivedResponsePackets(responseNaksList []UDPPacket, responsePackets []UDPPacket, udpConn *net.UDPConn, addr *net.UDPAddr) {
	for _, nakPack := range responseNaksList {
		missingNo := binary.BigEndian.Uint32(nakPack.seqNo)
		missingPacket := responsePackets[int(missingNo)-1]
		_, err := udpConn.WriteToUDP(getBytesFromPacket(missingPacket), addr)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func writeResponseToClient(httpPayload []string, totalNumPackets int, hostAddr string, hostPort int, udpConn *net.UDPConn, addr *net.UDPAddr) ([]UDPPacket, int) {
	stringifiedResponsePayload := getResponsePayload(httpPayload, totalNumPackets)
	var responsePackets []UDPPacket
	responsePacketsBytes, numOfResponsePackets := getResponsePacketBytes(1, hostAddr, uint16(hostPort), stringifiedResponsePayload)
	for _, packetBytes := range responsePacketsBytes {
		responsePackets = append(responsePackets, parsePacket(packetBytes))
		_, err := udpConn.WriteToUDP(packetBytes, addr)
		if err != nil {
			fmt.Println(err)
		}
	}
	return responsePackets, numOfResponsePackets
}

func timeOut(clients *sync.Map, hostAddr string) {
	client, ok := clients.LoadAndDelete(hostAddr)
	if !ok {
		LogInfo("Failed to remove client from sync map!")
	} else {
		close(client.(chan UDPPacket))
		LogInfo("Closing client channel for " + hostAddr)
	}
}

func remove(packetList []UDPPacket, removePack UDPPacket) []UDPPacket {
	for i, curr := range packetList {
		if binary.BigEndian.Uint32(curr.seqNo) == binary.BigEndian.Uint32(removePack.seqNo) {
			return append(packetList[:i], packetList[i+1:]...)
		}
	}
	return packetList
}

func getResponsePayload(httpPayload []string, totalNumPackets int) string {
	stringifiedPayload := stringifyRequestPayload(httpPayload, totalNumPackets)
	responsePayload := *handleUdpConnection(stringifiedPayload)
	return responsePayload
}

func getResponsePacketBytes(seqNo uint32, hostAddr string, port uint16, payload string) ([][]byte, int) {
	numPackets := int(math.Ceil(float64(len(payload)) / float64(1012)))
	packetsBytes := make([][]byte, numPackets)
	payloadBytes := []byte(payload)

	if numPackets == 1 {
		packetBytes := getBytesFromPacket(MakePacket(0, seqNo, hostAddr, port, payload))
		packetsBytes[0] = packetBytes
		packetsBytes[0] = append(packetsBytes[0], byte(1))
		return packetsBytes, 1
	}

	counter := 0
	for i := 1; i < numPackets; i++ {
		chunk := payloadBytes[counter : counter+1012]
		packetForChunk := MakePacket(0, seqNo, hostAddr, port, string(chunk))
		packetsBytes[i-1] = getBytesFromPacket(packetForChunk)
		packetsBytes[i-1] = append(packetsBytes[i-1], byte(numPackets))
		counter += 1012
		seqNo++
	}
	residue := len(payload) % 1012
	if residue > 0 {
		residueChunk := payloadBytes[counter:]
		packetsBytes[numPackets-1] = getBytesFromPacket(MakePacket(0, seqNo, hostAddr, port, string(residueChunk)))
		packetsBytes[numPackets-1] = append(packetsBytes[numPackets-1], byte(numPackets))
	}
	return packetsBytes, numPackets
}

func stringifyRequestPayload(httpPayload []string, totalNumPackets int) string {
	stringifiedHttpPayload := ""
	for _, packet := range httpPayload[4:(4 + totalNumPackets)] {
		stringifiedHttpPayload += packet
	}
	return stringifiedHttpPayload
}

func checkNotEmpty(httpPayload []string) bool {
	for _, packet := range httpPayload {
		if len(packet) == 0 {
			return false
		}
	}
	return true
}

func handleHandshakePacket(packet UDPPacket, addr *net.UDPAddr, conn *net.UDPConn) *int {
	hostAddr := getAddressFromBytes(packet)
	if packet.pType[0] == 2 {
		// SYN
		receivedSeq := binary.BigEndian.Uint32(packet.seqNo)
		totalNumPackets, err := strconv.Atoi(string(packet.payload))
		if err != nil {
			LogInfo("Corrupt SYN packet!")
		}
		synAck := MakePacket(3, receivedSeq+1, hostAddr, binary.BigEndian.Uint16(packet.peerPort), "")
		packetBytes := getBytesFromPacket(synAck)
		for {
			_, writeErr := conn.WriteToUDP(packetBytes, addr)
			if writeErr != nil {
				LogInfo("Timeout handshaking!")
				continue
			}
			break
		}
		LogInfo(fmt.Sprintf("SYN'd packet %d", receivedSeq))
		return &totalNumPackets
	} else if packet.pType[0] == 1 {
		// ACK
		receivedSeq := binary.BigEndian.Uint32(packet.seqNo)
		LogInfo(fmt.Sprintf("Received ACK for packet %d", receivedSeq))
	}
	return nil
}

func StartServer(port string, directory string, verbose bool) {
	listener, err := net.Listen("tcp", port)

	if _, err := os.Stat(directory); os.IsNotExist(err) {
		log.Fatal("Directory not found.")
	}

	rootDirectory = directory
	verboseLogging = verbose

	if err != nil {
		fmt.Println(err)
		return
	}

	defer listener.Close()

	for {
		curConn, err := listener.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		go handleConnection(curConn)
	}
}
