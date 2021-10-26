package main

// Built on the example file of github.com/mattn/go-mjpeg: https://github.com/mattn/go-mjpeg/blob/master/_example/camera/main.go

import (
	"context"
	"flag"
	"fmt"
	"image/color"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/mattn/go-mjpeg"
	"golang.org/x/time/rate"

	"gocv.io/x/gocv"
)

var (
	camera     = flag.String("camera", "0", "Camera ID")
	port       = flag.Int("port", 8080, "Server port")
	xml        = flag.String("classifier", "haarcascade_frontalface_default.xml", "classifier XML file")
	message    = flag.String("message", "", "The message to say if there are human faces detected.")
	show_faces = flag.Bool("show-faces", false, "Show the locations of detected face in the image stream")
	interval   = flag.Duration("interval", 30*time.Millisecond, "interval")
)

func capture(ctx context.Context, wg *sync.WaitGroup, stream *mjpeg.Stream) {
	defer wg.Done()

	var webcam *gocv.VideoCapture
	var err error
	if id, err := strconv.ParseInt(*camera, 10, 64); err == nil {
		webcam, err = gocv.VideoCaptureDevice(int(id))
	} else {
		webcam, err = gocv.VideoCaptureFile(*camera)
	}
	if err != nil {
		log.Println("unable to init web cam: %v", err)
		return
	}
	defer webcam.Close()

	classifier := gocv.NewCascadeClassifier()
	defer classifier.Close()
	if !classifier.Load(*xml) {
		log.Println("unable to load:", *xml)
		return
	}

	im := gocv.NewMat()

	var limiter = rate.NewLimiter(0.1, 1)

	for len(ctx.Done()) == 0 {
		var buf []byte
		if stream.NWatch() > 0 {
			if ok := webcam.Read(&im); !ok {
				continue
			}

			rects := classifier.DetectMultiScale(im)

			if len(rects) > 0 {
				if len(*message) > 0 && limiter.Allow() {
					println(*message)
					cmd := exec.Command("spd-say", "-r -30", *message)
					if err := cmd.Run(); err != nil {
						log.Fatal(err)
					}
				}
			}

			if *show_faces {
				for _, r := range rects {
					face := im.Region(r)
					face.Close()
					gocv.Rectangle(&im, r, color.RGBA{0, 0, 255, 0}, 2)
				}
			}

			nbuf, err := gocv.IMEncode(".jpg", im)
			if err != nil {
				continue
			}
			buf = nbuf.GetBytes()
		}
		err = stream.Update(buf)
		if err != nil {
			break
		}
	}
}

func main() {
	flag.Parse()

	stream := mjpeg.NewStreamWithInterval(*interval)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go capture(ctx, &wg, stream)

	http.HandleFunc("/mjpeg", stream.ServeHTTP)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<img src="/mjpeg" style="height: 100%;"/>`))
	})

	server := &http.Server{Addr: ":" + strconv.Itoa(*port)}
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt)
	go func() {
		<-sc
		server.Shutdown(ctx)
	}()
	fmt.Printf("Serving the camera stream at: http://0.0.0.0:%d\n", *port)
	server.ListenAndServe()
	stream.Close()
	cancel()

	wg.Wait()
}
