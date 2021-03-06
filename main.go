package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	// "internal/bytealg"
	// "unicode"
	// "unicode/utf8"

	// "encoding/csv"
	// "encoding/gob"
	// "github.com/boltdb/bolt"
	"github.com/ThomasK81/gocite"
	"github.com/gorilla/mux"

)

// ********SECTION 1: Parse AND Load Config File****************

// 1. server-config
// It's always better to give people to change stuff. Instead of hard-coding the port etc., parse a config file into this struct
// The config file could be JSON or XML or anything else really, but those two formats I recommend
type serverConfig struct {
	Host   string `json:"host"`
	Port   string `json:"port"`
	Source string `json:"cex_source"`
}

// reading in the info from the config.json into a serverConfig object
var confvar = loadConfiguration("./config.json")

func loadConfiguration(file string) serverConfig {
	var config serverConfig
	configFile, err := os.Open(file)
	defer configFile.Close()
	if err != nil {
		fmt.Println(err.Error())
	}
	jsonParser := json.NewDecoder(configFile)
	jsonParser.Decode(&config)
	return config
}



// ********SECTION 2: READ CORPUS****************

// we want to be able to have a corpus
// a corpus is a collection of documents / works
// we can use the gocite library to mimic a backend for this
// but firdst we need to be able to simply read from the interweb
// getContent does this
func getContent(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Status error: %v", resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Read body: %v", err)
	}

	return data, nil
}

var backend = []gocite.Work{}

func loaddata() {
	corpus := []gocite.Work{}
	data, _ := getContent(confvar.Source)
	str := string(data)
	// find the section that contains the textdata and split the string so it only contains that section
	ctsdata := strings.Split(str, "#!ctsdata")[1]
	ctsdata = strings.Split(ctsdata, "#!")[0]
	// fix multiline errors in the data
	re := regexp.MustCompile("(?m)[\r\n]*^//.*$")
	ctsdata = re.ReplaceAllString(ctsdata, "")

	reader := csv.NewReader(strings.NewReader(ctsdata))
	reader.Comma = '#'
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	var texturns, text []string

	for {
		line, error := reader.Read()
		if error == io.EOF {
			break
		} else if error != nil {
			fmt.Println(line)
			log.Fatal(error)
		}
		switch {
		// case to be expected
		case len(line) == 2:
			texturns = append(texturns, line[0])
			text = append(text, line[1])
		// in case the lazy quote does not work properly
		case len(line) > 2:
			texturns = append(texturns, line[0])
			var textstring string
			for j := 1; j < len(line); j++ {
				textstring = textstring + line[j]
			}
			text = append(text, textstring)
		// fatal data error
		case len(line) < 2:
			log.Fatal("Wrong line:", line)
		}
	}

	workid := ""
	newwork := gocite.Work{}
	count := 0
	for i, v := range texturns {
		texturn := gocite.SplitCTS(v)
		workurn := strings.Join([]string{texturn.Base, texturn.Protocol, texturn.Namespace, texturn.Work}, ":")
		if workurn != workid {
			if workid == "" {
				workid = workurn
				newwork.WorkID = workurn
				newPassage := gocite.Passage{PassageID: workurn + ":" + texturn.Passage, Text: gocite.EncText{CEX: text[i]}, Index: count}
				newwork.Passages = append(newwork.Passages, newPassage)
			} else {
				workid = workurn
				corpus = append(corpus, newwork)
				newwork = gocite.Work{}
				newwork.WorkID = workurn
				count = 0
				newPassage := gocite.Passage{PassageID: workurn + ":" + texturn.Passage, Text: gocite.EncText{CEX: text[i]}, Index: count}
				newwork.Passages = append(newwork.Passages, newPassage)
			}
		} else {
			newPassage := gocite.Passage{PassageID: workurn + ":" + texturn.Passage, Text: gocite.EncText{CEX: text[i]}, Index: count}
			newwork.Passages = append(newwork.Passages, newPassage)
		}
		count = count + 1
	}
	corpus = append(corpus, newwork)
	log.Println("corpus successfully read")
	backend = corpus
	return
}

func loadDB(w http.ResponseWriter, r *http.Request) {
	loaddata()
	io.WriteString(w, "Success")
}


//********SECTION 3: RABIN KARP STRING SEARCH****************

// primeRK is the prime base used in Rabin-Karp algorithm.
const primeRK = 16777619 

// hashStr returns the hash and the appropriate multiplicative
// factor for use in Rabin-Karp algorithm.
func hashStr(sep string) (uint32, uint32) {
	hash := uint32(0)
	for i := 0; i < len(sep); i++ {
		hash = hash*primeRK + uint32(sep[i])
	}
	var pow, sq uint32 = 1, primeRK
	for i := len(sep); i > 0; i >>= 1 {
		if i&1 != 0 {
			pow *= sq
		}
		sq *= sq
	}
	return hash, pow
}

// returns the index of the first instance of substr in s, or -1 if substr is not present in s.
func searchSubstrIndex(s, substr string) int {
	n := len(substr)
	switch {
	case n == 0:
		return 0
	case n == 1:
		return strings.IndexByte(s, substr[0])
	case n == len(s):
		if substr == s {
			return 0
		}
		return -1
	case n > len(s):
		return -1
	}
	c := substr[0]
	i := 0
	t := s[:len(s)-n+1]
	fails := 0
	for i < len(t) {
		if t[i] != c {
			o := strings.IndexByte(t[i:], c)
			if o < 0 {
				return -1
			}
			i += o
		}
		if s[i:i+n] == substr {
			return i
		}
		i++
		fails++
		if fails >= 4+i>>4 && i < len(t) {
			j := indexRabinKarp(s[i:], substr)
			if j < 0 {
				return -1
			}
			return i + j
		}
	}
	return -1
}

func indexRabinKarp(s, substr string) int {
	// Rabin-Karp search
	hashss, pow := hashStr(substr)
	n := len(substr)
	var h uint32
	for i := 0; i < n; i++ {
		h = h*primeRK + uint32(s[i])
	}
	if h == hashss && s[:n] == substr {
		return 0
	}
	for i := n; i < len(s); {
		h *= primeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i-n])
		i++
		if h == hashss && s[i-n:i] == substr {
			return i - n
		}
	}
	return -1

}

// ********SECTION 4: Substr search in corpus And GUI ****************
func searchCorpus(w http.ResponseWriter, r *http.Request){
	// ToDo

	log.Println("method:", r.Method) //get request method
    if r.Method == "GET" {
        loaddata()
        t, _ := template.ParseFiles("play.gtpl")
        t.Execute(w, nil)
    } else {
        r.ParseForm()

    	fmt.Println("SEARCH STRING:", r.Form["inputString"])
    	toMatchSubstr := string(r.Form["inputString"][0])
    	if(toMatchSubstr==""){
    		io.WriteString(w, "Input substring cannot be empty. #DoNotMessWithMe \n")
    		return
    	}
		count :=0
		for i := range backend {
			for j := range backend[i].Passages {
				passText := fmt.Sprint(backend[i].Passages[j].Text)
				if (searchSubstrIndex(passText, toMatchSubstr)>=0) {
					count+=1
					if(count==1){
						io.WriteString(w, "Input Substring is present in following:- \n\n")
					}
					io.WriteString(w, backend[i].Passages[j].PassageID+"\n")

				}
			}
		}
		if(count ==0 ){
			io.WriteString(w, "No matching passage texts found! #sorryNoSorry\n")
		}
    }

}


// ********SECTION 5: MAIN RUNNER****************
func main() {
	router := mux.NewRouter()
	// first thing we are going to do is to open a folder for external reading, so clients can read static files from there
	cexHandler := http.StripPrefix("/cex/", http.FileServer(http.Dir("./cex/")))
	router.PathPrefix("/cex/").Handler(cexHandler)

	// build the backend
	// router.HandleFunc("/loadDB", loadDB)

	// router.HandleFunc("/searchCorpus/{searchString}", searchCorpus)
	router.HandleFunc("/searchCorpus", searchCorpus)

	log.Println("Listening at" + confvar.Port + "...")
	log.Fatal(http.ListenAndServe(confvar.Port, router))
}



