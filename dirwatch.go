package dirwatch

import (
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

type Watcher struct {
	paths    map[string]struct{}
	add      chan string
	notifier func(fsnotify.Event)
	stop     chan struct{}
}

func NewWatcher(notifier func(fsnotify.Event)) *Watcher {
	if notifier == nil {
		panic("Notifier callback required. But was nil")
	}
	res := &Watcher{
		paths:    make(map[string]struct{}),
		add:      make(chan string),
		notifier: notifier,
		stop:     make(chan struct{}, 2),
	}
	return res
}

func (dw *Watcher) Start() {
	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Fatal("Could not start watcher", err)
		}
		defer watcher.Close()
		for {
			select {
			case <-dw.stop:
				return
			case ev := <-watcher.Events:
				dw.onEvent(watcher, ev)
			case err := <-watcher.Errors:
				log.Println("Error during watch: ", err)
			case dir := <-dw.add:
				dw.onAdd(watcher, dir)
			}
		}
	}()
}

func (dw *Watcher) Stop() {
	dw.stop <- struct{}{} //watcher routine
	dw.stop <- struct{}{} //adder routine
}

func (dw *Watcher) onEvent(watcher *fsnotify.Watcher, event fsnotify.Event) {
	go func() { dw.notifier(event) }()
	if event.Op&fsnotify.Remove == fsnotify.Remove {
		if _, watching := dw.paths[event.Name]; watching {
			dw.remove(watcher, event.Name)
		}
		return
	}
	file, err := os.Stat(event.Name)
	if err != nil {
		log.Printf("Cannot stat %v: %v", event.Name, err)
		return
	}
	if file.IsDir() {
		dw.Add(event.Name)
	}
}

func (dw *Watcher) remove(watcher *fsnotify.Watcher, path string) {
	log.Printf("removing watch on %v", path)
	delete(dw.paths, path)
	//removing kevent watch unnecessary: fsnotify seems to handle that
}

func (dw *Watcher) onAdd(watcher *fsnotify.Watcher, dir string) {
	if dir == "" {
		return
	}
	_, alreadyWatching := dw.paths[dir]
	if alreadyWatching {
		return
	}
	_, err := os.Stat(dir)
	if err != nil {
		log.Println(dir+" does not exist or is not accessible", err)
		return
	}
	if err := watcher.Add(dir); err != nil {
		log.Printf("Failed to add  %v: %v", dir, err)
	}
	log.Printf("added watch on %v", dir)
	dw.paths[dir] = struct{}{}
	dw.recurse(dir)
}

func (dw *Watcher) Add(dirs ...string) {
	go func() {

		for _, d := range dirs {
			d, err := filepath.Abs(d)
			if err != nil {
				log.Println("Could not create absolute path for "+d, err)
			}
			select {
			case dw.add <- d:
			case <-dw.stop:
				return
			}
		}
	}()
}

func (dw *Watcher) recurse(dir string) {
	go func() {
		files, err := ioutil.ReadDir(dir)
		if err != nil {
			log.Fatalf("Failed to recursively watch %v: %v", dir, err)
		}
		for _, file := range files {
			if file.IsDir() {
				dw.Add(filepath.Join(dir, file.Name()))
			}
		}
	}()
}
