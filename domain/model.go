package domain

import (
	"fmt"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type MetricsCount struct {
	RttSettings         float64 `json:"rtt_settings"`                 // настройки ртт которые задает пользователь (в милисекундах)
	PacketLossSettings  float64 `json:"packet_loss_settings_percent"` // настройки потери пакетов, которые задает пользователь (в пакетах)
	Rtt                 float64 `json:"rtt_ms"`                       // реальный показатель ртт
	PacketLoss          float64 `json:"packet_loss_percent"`          // реальный показатель потерянных пакетов
	AliveMainNetwork    bool    `json:"alive_main_network"`           // состояние основного сетевого интерфейса
	AliveReserveNetwork bool    `json:"alive_reserve_network"`        // состояние резервного сетевого интерфейса
	PingerCount         int     `json:"pinger_count"`                 // настройки количества пакетов при тестировании сети (пользователь)
	PingerInterval      int64   `json:"pinger_interval_ms"`           // настройки интервалов пинга (пользователь)
	NetworkSwitchMode   string  `json:"network_switch_mode"`          // настройки режима переключения сети
	CurrentInterface    string  `json:"current_interface"`
	PingBlocksNum       int     `json:"ping_blocks_num" validate:"numeric,required,min=1"`
}

type MetricsUserSetDto struct {
	RttSettings    float64 `json:"rtt_settings_ms" validate:"required,numeric,min=0"`
	PacketLoss     float64 `json:"packet_loss_percent" validate:"required,numeric,min=0,max=100"`
	PingerCount    int     `json:"pinger_count" validate:"required,numeric,min=1"`
	PingerInterval int64   `json:"pinger_interval_ms" validate:"numeric,required,min=20"`
	PingBlocksNum  int     `json:"ping_blocks_num" validate:"numeric,required,min=1"`
}
type NetworkSwitchSettingsUserSetDTO struct {
	NetworkSwitchMode string `json:"network_switch_mode" validate:"eq=main|eq=auto|eq=reserve,required"`
}

func (m *MetricsCount) AutoNetwork(ch chan struct{}) error {
	switchCount := 1
	switchCountPacket := 1
	IsMain := false
	IsReserve := false
	for m.NetworkSwitchMode == "auto" {
		<-ch

		if m.Rtt > m.RttSettings && switchCount == 0 {
			switchCount++
			if !IsReserve {
				if err := m.IpTablesSwitchReserve(); err != nil {
					return fmt.Errorf("auto switch err: %w", err)
				}
				IsReserve = true
				IsMain = false
			}
		} else if m.Rtt < m.RttSettings && switchCount == 1 {
			switchCount--
			if !IsMain {
				if err := m.IpTablesSwitchMain(); err != nil {
					return fmt.Errorf("auto switch err: %w", err)
				}
				IsMain = true
				IsReserve = false
			}
		}
		if m.PacketLoss > m.PacketLossSettings && switchCountPacket == 0 {
			switchCountPacket++
			if !IsReserve {
				if err := m.IpTablesSwitchReserve(); err != nil {
					return fmt.Errorf("auto switch err: %w", err)
				}
				IsReserve = true
				IsMain = false
			}
		} else if m.PacketLoss <= m.PacketLossSettings && switchCountPacket == 1 {
			switchCountPacket--
			if !IsMain {
				if err := m.IpTablesSwitchMain(); err != nil {
					return fmt.Errorf("auto switch err: %w", err)
				}
				IsMain = true
				IsReserve = false
			}
		}

	}

	return nil
}

// IpTablesSwitchMain
// запуск заранее подготовленного скрипта для очистки таблиц маршрутизации и
// загрузки новых под основную сеть
func (m *MetricsCount) IpTablesSwitchMain() error {
	_, mainErr := exec.Command("ifmetric", "eth0", "100").Output()
	if mainErr != nil {
		fmt.Println("while switching to main:", mainErr)
	}
	//m.CurrentInterface = "eth0"
	fmt.Println("switched to main")
	return nil
}

// IpTablesSwitchReserve
// запуск заранее подготовленного скрипта для очистки таблиц маршрутизации и
// загрузки новых под резервную сеть
func (m *MetricsCount) IpTablesSwitchReserve() error {
	_, reserveErr := exec.Command("ifmetric", "eth0", "102").Output()
	if reserveErr != nil {
		fmt.Println("while switching to reserve:", reserveErr)
	}
	//m.CurrentInterface = "wan0"
	fmt.Println("switched to reserve")
	return nil
}

func (m *MetricsCount) Pinger() (finalPacketLoss float64, finalRtt float64,
	PingerErr error) {
	for i := m.PingBlocksNum; i != 0; i-- {
		ping, err := exec.Command("ping", "-I", m.CurrentInterface, "-i 0.2",
			"-c 10", "8.8.8.8").Output()
		if err != nil {
			log.Println("while pinging: ", err)
		}
		stringPing := string(ping)
		packetLoss := strings.Split(stringPing, "\n")
		rttRow := packetLoss[len(packetLoss)-2]
		packetLossRow := packetLoss[len(packetLoss)-3]
		splittedPacketLossRow := strings.Split(packetLossRow, ",")
		loss, PingerErr := strconv.ParseFloat(string(
			splittedPacketLossRow[2][1]), 64)
		if err != nil {
			log.Println(PingerErr)
		}
		finalPacketLoss += loss
		splittedRttRow := strings.Split(rttRow, "/")
		parseRtt := splittedRttRow[3]
		tt := strings.Split(parseRtt, " ")
		rtt, PingerErr := strconv.ParseFloat(tt[2], 64)
		if err != nil {
			log.Println(PingerErr)
		}
		finalRtt += rtt

	}
	t := finalRtt / float64(m.PingBlocksNum)
	finalRtt = float64(int(t*100)) / 100
	finalPacketLoss = finalPacketLoss / float64(m.PingBlocksNum)

	return finalPacketLoss, finalRtt, nil
}

func (m *MetricsCount) SetDefaultFromEnv() (err error) {

	fmt.Println(unix.Getenv("RTT_SETTINGS"))

	var defaultEnv = []string{"RTT_SETTINGS", "PACKET_LOSS_SETTINGS", "PINGER_COUNT",
		"PINGER_INTERVAL", "NETWORK_SWITCH_MODE", "PING_BLOCKS_NUM"}

	for _, env := range defaultEnv {
		val, found := unix.Getenv(env)
		if !found {
			log.Printf("founded env: %b, %s\n", found, env)
			log.Println("there is no default env in /etc/environment, stop.")
			os.Exit(1)
		}

		switch env {
		case "RTT_SETTINGS":
			m.RttSettings, _ = strconv.ParseFloat(val, 10)
		case "PACKET_LOSS_SETTINGS":
			m.PacketLossSettings, _ = strconv.ParseFloat(val, 10)
		case "PINGER_COUNT":
			m.PingerCount, _ = strconv.Atoi(val)
		case "PINGER_INTERVAL":
			m.PingerInterval, _ = strconv.ParseInt(val, 64, 10)
		case "NETWORK_SWITCH_MODE":
			m.NetworkSwitchMode = val
		case "PING_BLOCKS_NUM":
			m.PingBlocksNum, _ = strconv.Atoi(val)
		default:

		}
	}
	return nil
}
