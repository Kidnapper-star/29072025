package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CfgData struct {
	Port       string   `json:"port"`
	FileTypes  []string `json:"file_types"`
	TempFolder string   `json:"temp_folder"`
}

type TaskStatus string

const (
	Pending    TaskStatus = "waiting"
	InProgress TaskStatus = "in_progress"
	Done       TaskStatus = "done"
	ErrStatus  TaskStatus = "err"
)

type Task struct {
	ID      string     `json:"id"`
	Files   []string   `json:"files"`
	Status  TaskStatus `json:"status"`
	Archive string     `json:"archive"`
	Err     string     `json:"err"`
	Created time.Time  `json:"created"`
}

var (
	cfg      CfgData
	alltasks = make(map[string]*Task)
	mux      sync.Mutex
)

// Точка входа в программу
func main() {
	loadCfg() // Загружаем настройки

	err := os.MkdirAll(cfg.TempFolder, 0755) // Создаем временную папку, если ее нет
	if err != nil {
		log.Fatalf("Can't create folder: %v", err)
	}

	http.HandleFunc("/tasks", createTask)      // Создание новой задачи
	http.HandleFunc("/tasks/", taskHandler)    // Обработка задач: статус или добавление файлов
	http.HandleFunc("/archives/", sendArchive) // Отдача архива по ссылке

	log.Println("serverport:", cfg.Port)              // Запуск сервера
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil)) // Запуск HTTP сервера
}

// Загружаем настройки из файла config.json или ставим значения по умолчанию
func loadCfg() {
	f, err := os.Open("config.json")
	if err != nil {
		cfg = CfgData{
			Port:       "8080",
			FileTypes:  []string{".jpeg", ".pdf"},
			TempFolder: "files",
		}
		return
	}
	defer f.Close()

	var tmp CfgData
	err = json.NewDecoder(f).Decode(&tmp)
	if err != nil || tmp.Port == "" {
		tmp.Port = "8080"
	}
	if len(tmp.FileTypes) == 0 {
		tmp.FileTypes = []string{".jpeg", ".pdf"}
	}
	if tmp.TempFolder == "" {
		tmp.TempFolder = "files"
	}

	cfg = tmp
}

// Создаем новую задачу (POST /tasks)
func createTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method is not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	mux.Lock()
	defer mux.Unlock()

	active := 0
	for _, t := range alltasks {
		if t.Status == Pending || t.Status == InProgress {
			active++
		}
	}
	if active >= 3 {
		http.Error(w, `{"error":"server busy"}`, http.StatusTooManyRequests)
		return
	}

	id := time.Now().Format("20060102150405")
	alltasks[id] = &Task{
		ID:      id,
		Files:   []string{},
		Status:  Pending,
		Created: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// Обработка запроса по задачам: получить статус или добавить файлы
func taskHandler(w http.ResponseWriter, r *http.Request) {
	pathPart := strings.TrimPrefix(r.URL.Path, "/tasks/")
	parts := strings.Split(pathPart, "/")

	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, `{"error":"id needed"}`, http.StatusBadRequest)
		return
	}

	id := parts[0]

	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		getStatus(w, r, id)
	case len(parts) == 2 && parts[1] == "files" && r.Method == http.MethodPost:
		addFiles(w, r, id)
	default:
		http.Error(w, `{"error":"cant find"}`, http.StatusNotFound)
	}
}

// Отдать статус задачи
func getStatus(w http.ResponseWriter, _ *http.Request, id string) {
	mux.Lock()
	t, ok := alltasks[id]
	mux.Unlock()
	if !ok {
		http.Error(w, `{"error":"task missing"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

// Добавить ссылки на файлы для скачивания в задачу
func addFiles(w http.ResponseWriter, r *http.Request, id string) {
	mux.Lock()
	t, ok := alltasks[id]
	mux.Unlock()
	if !ok {
		http.Error(w, `{"error":"task missing"}`, http.StatusNotFound)
		return
	}
	if t.Status == Done {
		http.Error(w, `{"error":"task already done"}`, http.StatusConflict)
		return
	}

	var urls []string
	if err := json.NewDecoder(r.Body).Decode(&urls); err != nil {
		http.Error(w, `{"error":"invalid format"}`, http.StatusBadRequest)
		return
	}
	if len(urls) == 0 {
		http.Error(w, `{"error":"empty"}`, http.StatusBadRequest)
		return
	}

	var allowedUrls []string
	var rejectedExts []string

	for _, url := range urls {
		ext := strings.ToLower(path.Ext(url))
		allow := false
		for _, ft := range cfg.FileTypes {
			if ext == ft {
				allow = true
				break
			}
		}
		if allow {
			allowedUrls = append(allowedUrls, url)
		} else {
			rejectedExts = append(rejectedExts, ext)
		}
	}

	if len(allowedUrls) == 0 {
		http.Error(w, `{"error":"only .pdf and .jpeg allowed: `+strings.Join(cfg.FileTypes, ", ")+`"}`, http.StatusUnsupportedMediaType)
		return
	}

	msg := "files ok"
	if len(rejectedExts) > 0 {
		msg += "; skipped: " + strings.Join(rejectedExts, ", ")
	}

	mux.Lock()
	t.Status = InProgress
	mux.Unlock()

	go process(id, allowedUrls)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": msg})
}

// Обработка задачи: скачиваем файлы и собираем архив
func process(id string, urls []string) {
	mux.Lock()
	t, ok := alltasks[id]
	mux.Unlock()
	if !ok {
		return
	}

	var local []string

	for i, url := range urls {
		resp, err := http.Get(url)
		if err != nil || resp.StatusCode != http.StatusOK {
			setError(t, "can't download "+url)
			return
		}
		defer resp.Body.Close()

		ext := path.Ext(url)
		fname := path.Join(cfg.TempFolder, id+"_"+time.Now().Format("150405")+"_"+strconv.Itoa(i)+ext)
		f, err := os.Create(fname)
		if err != nil {
			setError(t, "can't create "+fname)
			return
		}
		_, err = io.Copy(f, resp.Body)
		f.Close()
		if err != nil {
			setError(t, "can't save "+fname)
			return
		}
		local = append(local, fname)
	}

	mux.Lock()
	t.Files = local
	mux.Unlock()

	zipfile := path.Join(cfg.TempFolder, id+".zip")
	zf, err := os.Create(zipfile)
	if err != nil {
		setError(t, "can't create archive")
		return
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	for _, fname := range local {
		f, err := os.Open(fname)
		if err != nil {
			continue
		}
		w, err := zw.Create(path.Base(fname))
		if err != nil {
			f.Close()
			continue
		}
		_, err = io.Copy(w, f)
		f.Close()
		if err != nil {
			continue
		}
	}
	err = zw.Close()
	if err != nil {
		setError(t, "error closing archive")
		return
	}

	mux.Lock()
	t.Status = Done
	t.Archive = zipfile
	mux.Unlock()
}

// Помечаем задачу как ошибочную и пишем сообщение
func setError(t *Task, msg string) {
	mux.Lock()
	defer mux.Unlock()
	t.Status = ErrStatus
	t.Err = msg
	log.Println("error in task:", msg)
}

// Отдаем архив по ссылке
func sendArchive(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimPrefix(r.URL.Path, "/archives/")
	fp := path.Join(cfg.TempFolder, file)
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		http.Error(w, `{"error":"archive is not found"}`, http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, fp)
}
