package service

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"log"
	"net"
	"net/http"
	"networkSwitcher/domain"
	"sync"
	"time"
)

// StartService - общий запуск приложения
func StartService() error {
	PingToSwitch := make(chan struct{})
	var set domain.MetricsCount
	validate := validator.New()
	// дефолтное значение параметров запуска утилиты
	_ = set.SetDefaultFromEnv()
	set.CurrentInterface = "eth0"
	wg := sync.WaitGroup{}
	wg.Add(4)
	r := gin.Default()
	if err := Endpoints(r, &wg, validate, &set); err != nil {
		return err
	}
	if err := NetworkScan(PingToSwitch, &set); err != nil {
		return err
	}
	if err := Switch(PingToSwitch, &set); err != nil {
		log.Println("switch: ", err)
	}
	wg.Wait()

	return nil
}

// Endpoints - все эндпоинты, которые держит джин, ничего особенного
func Endpoints(r *gin.Engine, wg *sync.WaitGroup, validate *validator.Validate,
	set *domain.MetricsCount) error {
	go func() {
		// роут для получения пользователем информации о системе и настройках
		r.GET("/get_info", func(c *gin.Context) {
			c.JSON(http.StatusOK, set)
		})
		r.POST("/configure", func(c *gin.Context) {
			var newSettings domain.MetricsUserSetDto
			if err := c.BindJSON(&newSettings); err != nil {
				c.IndentedJSON(http.StatusBadRequest, fmt.Sprintf("validation:"+
					" %v", err))
			}
			if err := validate.Struct(newSettings); err != nil {
				c.IndentedJSON(http.StatusBadRequest, fmt.Sprintf("validation:"+
					" %v", err))
			} else {
				set.PacketLossSettings = newSettings.PacketLoss
				set.RttSettings = newSettings.RttSettings
				set.PingerCount = newSettings.PingerCount
				set.PingerInterval = newSettings.PingerInterval
				set.PingBlocksNum = newSettings.PingBlocksNum
				c.IndentedJSON(http.StatusCreated, set)
			}

		})
		// выбор режима сети
		r.POST("/set_network_mode", func(c *gin.Context) {

			var networkSwitchMode domain.NetworkSwitchSettingsUserSetDTO
			if err := c.BindJSON(&networkSwitchMode); err != nil {
				return
			}
			errs := validate.Struct(&networkSwitchMode)
			if errs != nil {
				c.IndentedJSON(http.StatusBadRequest, "bad validation:")
				log.Println("bad validation while changing network switch mode")
			} else {

				set.NetworkSwitchMode = networkSwitchMode.NetworkSwitchMode

				c.IndentedJSON(http.StatusAccepted,
					"network mode: "+set.NetworkSwitchMode)
				log.Println("network switch mode changer by user: ",
					set.NetworkSwitchMode)
			}
		})
		err := r.Run()
		if err != nil {
			log.Panicf("failed to start server: %s", err)
		} else {
		}
		wg.Done()
	}()
	return nil
}

// NetworkScan - инкапсулирует в себе функции сканирования состояния сети,
// их обработки, сравнения с опорными значениями, заданными пользователем через
// переменные среды, либо через api (/configure)
func NetworkScan(PingToSwitch chan struct{}, set *domain.MetricsCount) error {
	go func() {
		var onc sync.Once
		for {
			_, err := net.DialTimeout("tcp", "google.com:80",
				time.Second*2)
			if err != nil {
				onc.Do(func() {
					err := set.IpTablesSwitchReserve()
					if err != nil {
						log.Println("while the main channel id not fine - cannot" +
							"switch to reserve channel")
					}
				})
				time.Sleep(time.Millisecond * 500)
				continue
			}
			finalPacketLoss, finalRtt, err := set.Pinger()
			if err != nil {
				log.Println("pinger func err:", err)
			}
			set.Rtt = finalRtt
			set.PacketLoss = finalPacketLoss * 10
			PingToSwitch <- struct{}{}
		}
	}()
	return nil
}

// Switch - функция переключения сети в зависимости от выставленных опорных
// значений, заданных пользователем, переключение реализовано в трех режимах
// auto, main, reserve
func Switch(PingToSwitch chan struct{}, set *domain.MetricsCount) error {
	go func() {
		auto := false
		main := false
		reserve := false
		for {
			if set.NetworkSwitchMode == "auto" && !auto {

				if err := set.AutoNetwork(PingToSwitch); err != nil {
					log.Println(err)
				}
				auto = true
				main = false
				reserve = false
			}
			if set.NetworkSwitchMode == "reserve" && !reserve {
				if err := set.IpTablesSwitchReserve(); err != nil {
					log.Println(err)
				}
				auto = false
				main = false
				reserve = true
				for set.NetworkSwitchMode == "reserve" {
					<-PingToSwitch
				}
			}
			if set.NetworkSwitchMode == "main" && !main {
				if err := set.IpTablesSwitchMain(); err != nil {
					log.Println(err)
				}
				auto = false
				reserve = false
				main = true
				for set.NetworkSwitchMode == "main" {
					<-PingToSwitch
				}
			}
			<-PingToSwitch
		}
	}()
	return nil
}
