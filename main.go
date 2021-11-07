package main

// Built on the example file of github.com/mattn/go-mjpeg: https://github.com/mattn/go-mjpeg/blob/master/_example/camera/main.go

import (
	"context"
	"flag"
	"image/color"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"time"

	auth "github.com/abbot/go-http-auth"
	"github.com/mattn/go-mjpeg"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"

	"gocv.io/x/gocv"
)

var (
	camera     = flag.String("camera", "0", "Camera ID")
	port       = flag.Int("port", 8080, "Server port")
	xml        = flag.String("classifier", "haarcascade_frontalface_default.xml", "classifier XML file")
	message    = flag.String("message", "", "The message to say if there are human faces detected.")
	show_faces = flag.Bool("show-faces", false, "Show the locations of detected face in the image stream.")
	interval   = flag.Duration("interval", 30*time.Millisecond, "interval")
	password   = flag.String("password", "", "The password to be used to access the HTTP video stream. ")
)

func capture(ctx context.Context, wg *sync.WaitGroup, stream *mjpeg.Stream) {
	defer wg.Done()

	var webcam *gocv.VideoCapture
	var capture_err error
	if id, err := strconv.ParseInt(*camera, 10, 64); err == nil {
		webcam, capture_err = gocv.VideoCaptureDevice(int(id))
	} else {
		webcam, capture_err = gocv.VideoCaptureFile(*camera)
	}
	if capture_err != nil {
		log.Printf("[ERROR]: Unable to init web cam: '%v'", capture_err)
		return
	}
	defer webcam.Close()

	var classifier_loaded bool
	classifier := gocv.NewCascadeClassifier()
	defer classifier.Close()
	if !classifier.Load(*xml) {
		log.Printf("[WARN]: Unable to load: %s. Face detection deactivated.", *xml)
		classifier_loaded = false
	} else {
		classifier_loaded = true
	}

	im := gocv.NewMat()

	var limiter = rate.NewLimiter(0.1, 1)

	for len(ctx.Done()) == 0 {
		var buf []byte
		if stream.NWatch() > 0 {
			if ok := webcam.Read(&im); !ok {
				continue
			}

			if classifier_loaded {
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
			}

			nbuf, err := gocv.IMEncode(".jpg", im)
			if err != nil {
				continue
			}
			buf = nbuf.GetBytes()
		}
		err := stream.Update(buf)
		if err != nil {
			break
		}
	}
}

func handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte(`<img src="/mjpeg" style="height: 100%;"/>`)); err != nil {
		log.Println("[ERROR]: Failed to write into the HTTP response.")
	}
}

func main() {
	flag.Parse()

	var secret = func(user, realm string) string {
		if user == "go" {
			hashedPassword, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
			if err == nil {
				return string(hashedPassword)
			}
		}
		return ""
	}
	authenticator := auth.NewBasicAuthenticator("go_web_video_server.com", secret)

	stream := mjpeg.NewStreamWithInterval(*interval)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go capture(ctx, &wg, stream)

	if len(*password) > 0 {
		log.Printf("[INFO]: Website username <go>, password <%s>", *password)
		http.HandleFunc("/", auth.JustCheck(authenticator, handle))
	} else {
		http.HandleFunc("/", handle)
	}

	http.HandleFunc("/mjpeg", stream.ServeHTTP)

	server := &http.Server{Addr: ":" + strconv.Itoa(*port)}
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt)
	go func() {
		<-sc
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("[DEBUG]: HTTP server '%v'.", err)
		}
	}()
	log.Printf("[INFO]: Serving the camera stream at: http://0.0.0.0:%d", *port)
	if err := server.ListenAndServe(); err != nil {
		log.Printf("[DEBUG]: HTTP server '%v'.", err)
	}
	stream.Close()
	cancel()

	wg.Wait()
}
