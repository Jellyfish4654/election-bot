package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/forms/v1"
	"google.golang.org/api/option"
)

type Credentials struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
}

func handle_start_appliction(client *http.Client) {
	service, err := forms.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		panic(err)
	}
}

func main() {
	// flag parsing
	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		fmt.Fprintf(os.Stderr, "usage: %s [ACTION] [OPTIONS...]\n\tpossible actions: start-application, start-vote, end-vote\n", os.Args[0])
	}
	subcommand := os.Args[1]
	if subcommand != "start-application" && subcommand != "start-vote" && subcommand != "end-vote" {
		fmt.Fprintln(os.Stderr, "invalid action. type "+os.Args[0]+" --help for more information")
	}

	// credentials
	var creds map[string]Credentials
	file, err := ioutil.ReadFile("./creds.json")
	if err != nil {
		panic(err)
	}
	json.Unmarshal(file, &creds)

	var cred Credentials
	for _, c := range creds {
		cred = c
	}

	config := &oauth2.Config{
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
		RedirectURL:  "http://127.0.0.1:4444/redirect",
		Scopes: []string{
			"https://www.googleapis.com/auth/forms.body",
			"https://www.googleapis.com/auth/spreadsheets",
		},
		Endpoint: google.Endpoint,
	}

	rand.Seed(time.Now().UnixNano())
	state := strconv.FormatUint(uint64(rand.Int63()), 36)
	auth_url := config.AuthCodeURL(state)

	// serve
	fmt.Printf("Open the following URL: %s\n", "http://127.0.0.1:4444/auth")

	r := mux.NewRouter()
	r.HandleFunc("/redirect", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if state == q.Get("state") {
			tok, err := config.Exchange(oauth2.NoContext, q.Get("code"))
			if err != nil {
				panic(err)
			}
			client := config.Client(oauth2.NoContext, tok)

			switch subcommand {
			case "start-application":
				handle_start_appliction(client)
			case "start-vote":
			case "end-vote":
			}
		}
	})

	r.Handle("/auth", http.RedirectHandler(auth_url, 307))

	http.Handle("/", r)
	http.ListenAndServe("localhost:4444", nil)
}
