package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"modules/internal/components/about"
	"modules/internal/components/contact"
	project "modules/internal/components/projects"

	//"modules/internal/components/project"

	"github.com/a-h/templ"
	"github.com/gorilla/mux"
)

type spaHandler struct {
	staticPath string
	indexPath  string
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(h.staticPath, r.URL.Path)
	file, err := os.Stat(path)
	if os.IsNotExist(err) || file.IsDir() {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

type QueryParams struct {
	id string
}

type HttpLog struct {
	LogID          uint64              `json:"logId"`
	TimeStamp      string              `json:"timeStamp"`
	Method         string              `json:"method"`
	URL            string              `json:"url"`
	Headers        map[string][]string `json:"headers"`
	QueryParams    QueryParams         `json:"queryParams"`
	ResponseTimeMs string              `json:"responseTimeMs"`
}

var (
	logQueue     = make(chan []HttpLog, 1)
	logBatchSize = 25
	logFilePath  = "./logs/HttpRequestLogs.json"
	logMutex     sync.Mutex
	catalogue    []HttpLog
)

var logIdCounter uint64

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		logEntry := HttpLog{
			LogID:     atomic.AddUint64(&logIdCounter, 1),
			TimeStamp: startTime.Format(time.RFC3339),
			Method:    r.Method,
			URL:       r.URL.String(),
			Headers:   r.Header,
			QueryParams: QueryParams{
				id: r.URL.Query().Get("id"),
			},
			ResponseTimeMs: fmt.Sprintf("%d", time.Since(startTime).Milliseconds()),
		}

		next.ServeHTTP(w, r)

		logMutex.Lock()
		defer logMutex.Unlock()

		catalogue = append(catalogue, logEntry)
		if len(catalogue) >= logBatchSize {
			batch := catalogue[:logBatchSize]
			catalogue = catalogue[logBatchSize:]
			logQueue <- batch
		}
	})
}

func writeToFile(logs []HttpLog) {
	data, err := json.Marshal(logs)
	if err != nil {
		log.Fatalf("error marshalling json from struct: %v", err)
	}

	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}

	if _, err := f.Write(data); err != nil {
		log.Fatalf("error writing to file: %v", err)
	}

	if err := f.Close(); err != nil {
		log.Fatalf("error closing file: %v", err)
	}
}

func writeCatalogueToFile() {
	logMutex.Lock()
	defer logMutex.Unlock()

	if len(catalogue) > 0 {
		batch := catalogue
		catalogue = nil
		writeToFile(batch)
	}
}

func archiveLogFile() {
	log.Println("Writing log to archive...")

	logMutex.Lock()
	defer logMutex.Unlock()

	path := "./logs/HttpRequestLogs.json"

	f, err := os.Open(path)

	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	read := bufio.NewReader(f)
	data, err := io.ReadAll(read)
	if err != nil {
		log.Fatalf("error reading file: %v", err)
	}

	f.Close()
	//Clear log.json after reading data
	f, err = os.OpenFile(path, os.O_RDWR|os.O_TRUNC, 0666)
	if err == nil {
		f.Close()
	} else {
		log.Fatalf("error opening file: %v", err)
	}

	now := time.Now().Format(time.DateOnly)

	archive_path := filepath.Join("logs/archive/", now+".gz")
	f, err = os.Create(archive_path)
	if err != nil {
		log.Fatalf("error creating archive: %v", err)
	}

	defer f.Close()

	if err != nil {
		log.Fatalf("error creating archive: %v", err)
	}

	w := gzip.NewWriter(f)
	defer w.Close()
	w.Write(data)
}

func LogHandler() {
	timer_write := time.NewTicker(15 * time.Minute)
	defer timer_write.Stop()

	timer_archive := time.NewTicker(24 * time.Hour)
	defer timer_archive.Stop()

	for {
		select {
		case batch := <-logQueue:
			log.Println("Writing batch of logs to file...")
			writeToFile(batch)
		case <-timer_write.C:
			writeCatalogueToFile()
		case <-timer_archive.C:
			writeCatalogueToFile()
			archiveLogFile()
		}
	}
}

func main() {
	log.Println("Starting...")

	r := mux.NewRouter()
	r.Use(LoggingMiddleware)
	r.PathPrefix("/resources/").Handler(http.StripPrefix("/resources", http.FileServer(http.Dir("./resources"))))

	var P = []about.Paragraph{
		{
			Title:   "EDUCATION",
			Content: "I have studied for 5 years at Umeå university where i've developed skills in software developement, especially technologies related to web developement and during my later years security and cryptography.",
		},

		{
			Title:   "PERSONAL LIFE",
			Content: "I was born the year 1999 and been pretty busy ever since. I enjoy Martial Arts, Bouldering, Music, Computers and Games",
		},
	}
	r.PathPrefix("/About").Handler(templ.Handler(about.Agregate(P)))

	var L = []contact.Link{
		{
			URL:  "mailto:melhen12344@gmail.com",
			Name: "melhen12344@gmail.com",
		},
		{
			URL:  "https://www.linkedin.com/in/melker-henriksson?lipi=urn%3Ali%3Apage%3Ad_flagship3_profile_view_base_contact_details%3BduMS0lLfSTq4LiRoBEqSJw%3D%3D",
			Name: "linkedin.com/in/melker-henriksson",
		},
		{
			URL:  "https://github.com/Henrikswoon",
			Name: "https://github.com/Henrikswoon",
		},
	}
	r.PathPrefix("/Contact").Handler(templ.Handler(contact.Display(L)))

	var A = []project.Article{
		{
			Title:   "Portfolio",
			Content: "I decided to make this portfolio using Go, HTMX, Templ and Sass as i have been interested in these technologies. I have found GO+HTMX to be a nice opportunity as they deal with 'the nitty gritty' more so than many .js frameworks i have worked with (although i was pretty tempted to write this in Next.js instead)\n",
			URL:     "melker.dev",
		},
		{
			Title:   "HearthHaven",
			Content: "A game that i want to make, will write more about it given that it is further developed",
			URL:     "https://github.com/Henrikswoon/Hearth-haven",
		},
	}
	r.PathPrefix("/Projects").Handler(templ.Handler(project.Display(A)))

	srv := &http.Server{
		Handler:      r,
		Addr:         "127.0.0.1:8000",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("error starting server: %v", err)
		}
	}()

	go LogHandler()

	spa := spaHandler{staticPath: "static", indexPath: "index.html"}
	r.PathPrefix("/").Handler(spa)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan //Block until user interupts or terminates process
	//https://emretanriverdi.medium.com/graceful-shutdown-in-go-c106fe1a99d9
	//https://dev.to/mokiat/proper-http-shutdown-in-go-3fji
	writeCatalogueToFile()

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), 10*time.Second)
	shutdownRelease()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("error closing server: %v", err)
	} else {
		log.Println("Closing...")
	}
}
