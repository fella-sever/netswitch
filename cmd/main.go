package main

import (
	"log"
	"networkSwitcher/service"
)

func main() {
	err := service.StartService()
	if err != nil {
		log.Println(err)
	}

}
