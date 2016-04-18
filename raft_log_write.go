package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	raft "github.com/caiopo/pontoon"
)

const (
	PORT                = ":55123"
	kubernetesAPIServer = "192.168.15.150:8080"
	log_to_file         = false
)

func main() {
	var file *os.File
	var writer *bufio.Writer

	if log_to_file {
		file, err := os.Create("/home/raft_log")

		if err != nil {
			panic(err)
		}

		writer = bufio.NewWriter(file)

		defer file.Sync()
		defer writer.Flush()

		log.SetOutput(writer)
	}

	myip := getMyIP("18")

	fmt.Println(myip)

	if myip == "badIPReturn" {
		myip = "localhost"
	}

	transport := &raft.HTTPTransport{Address: myip + PORT}
	logger := &raft.Log{}
	applyer := &raft.StateMachine{}
	node := raft.NewNode(myip, transport, logger, applyer)
	node.Serve()

	node.Start()
	defer node.Exit()

	ipsAdded := make([]string, 0)

	for {
		ipsKube := getIPsFromKubernetes()

		fmt.Print("IPs Kube: ")
		fmt.Println(ipsKube)

		fmt.Print("IPs Added: ")
		fmt.Println(ipsAdded)

		for _, ipKube := range ipsKube {
			if !find(ipKube, ipsAdded) && (ipKube != myip) {
				node.AddToCluster(ipKube + PORT)
				ipsAdded = append(ipsAdded, (ipKube))
			}
		}

		if log_to_file {
			writer.Flush()
			file.Sync()
		}

		time.Sleep(time.Second)
	}

}

func find(needle string, haystack []string) bool {
	for _, h := range haystack {
		if needle == h {
			return true
		}
	}
	return false
}

func getIPsFromKubernetes() []string {
	resp, err := http.Get("http://" + kubernetesAPIServer + "/api/v1/endpoints")

	if err != nil {
		raft.Debug += fmt.Sprintln("ERROR getting endpoints in kubernetes API: ", err.Error())
		return nil
	}
	defer resp.Body.Close()
	contentByte, err2 := ioutil.ReadAll(resp.Body)
	if err2 != nil {
		raft.Debug += fmt.Sprintln("ERROR reading data from endpoints: ", err2.Error())
		return nil
	}

	content := string(contentByte)

	replicas := make([]string, 0)

	words := strings.Split(content, "\"ip\":")
	for _, v := range words {
		if v[1:7] == "18.16." { //18.x.x.x, IP of PODS
			parts := strings.Split(v, ",")
			theIP := parts[0]
			theIP = theIP[1 : len(theIP)-1] //remove " chars from IP
			replicas = append(replicas, theIP)
		}
	}

	return replicas
}

func getMyIP(firstChars string) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "couldNotConfigurateIP:" + err.Error()
	} else {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					s := ipnet.IP.String()
					if s[:len(firstChars)] == firstChars {
						return s
					}
				}
			}
		}
	}
	return "badIPReturn"
}

func createNodes(num int) []*raft.Node {
	var nodes []*raft.Node

	for i := 0; i < num; i++ {
		transport := &raft.HTTPTransport{Address: "127.0.20.0:5123" + strconv.Itoa(i)}
		logger := &raft.Log{}
		applyer := &raft.StateMachine{}
		node := raft.NewNode(fmt.Sprintf("%d", i), transport, logger, applyer)
		nodes = append(nodes, node)
		nodes[i].Serve()
	}

	// let them start serving
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < len(nodes); i++ {
		for j := 0; j < len(nodes); j++ {
			if j != i {
				nodes[i].AddToCluster(nodes[j].Transport.String())
			}
		}
	}

	for _, node := range nodes {
		node.Start()
	}

	return nodes
}

func countLeaders(nodes []*raft.Node) int {
	leaders := 0
	for i := 0; i < len(nodes); i++ {
		nodes[i].RLock()
		if nodes[i].State == raft.Leader {
			leaders++
		}
		nodes[i].RUnlock()
	}
	return leaders
}

func findLeader(nodes []*raft.Node) *raft.Node {
	for i := 0; i < len(nodes); i++ {
		nodes[i].RLock()
		if nodes[i].State == raft.Leader {
			nodes[i].RUnlock()
			return nodes[i]
		}
		nodes[i].RUnlock()
	}
	return nil
}

func startCluster(num int) ([]*raft.Node, *raft.Node) {
	nodes := createNodes(num)
	for {
		time.Sleep(50 * time.Millisecond)
		if countLeaders(nodes) == 1 {
			break
		}
	}
	leader := findLeader(nodes)
	return nodes, leader
}

func stopCluster(nodes []*raft.Node) {
	for _, node := range nodes {
		node.Exit()
	}
}
