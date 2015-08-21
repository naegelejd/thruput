package main

import (
	"fmt"
	"io"
	"log"
	"net"
)

type Server interface {
	Run() error
}

func NewServer(protocol, address string) (s Server, err error) {
	switch protocol {

	case "tcp":
		s = TCPServer{protocol, address}
	case "udp":
		s = UDPServer{protocol, address}
	default:
		err = fmt.Errorf("unknown protocol")
	}
	return
}

type server struct {
	protocol string
	address  string
}
type TCPServer server
type UDPServer server

func readForever(c net.Conn) {
	defer func() {
		log.Println("Closing connection to", c.RemoteAddr())
		c.Close()
	}()

	log.Println("Reading from connection", c.RemoteAddr())
	buffer := make([]byte, bufsize)
	for {
		_, err := c.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Println(err)
			}
			break
		}
	}
}

func (s TCPServer) Run() error {
	ln, err := net.Listen(s.protocol, s.address)
	if err != nil {
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go readForever(conn)
	}
}

func (s UDPServer) Run() error {
	udpaddr, err := net.ResolveUDPAddr(s.protocol, s.address)
	if err != nil {
		return err
	}
	for {
		conn, err := net.ListenUDP(s.protocol, udpaddr)
		if err != nil {
			return err
		}
		readForever(conn)
	}
}
