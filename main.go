package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	// sanity check
	if _, err := os.Stat("state/application.txt"); err == nil {
		fmt.Println("`state/application.txt' already exists, meaning you've already created the application form! To reset the process, delete the `state' folder.")
		os.Exit(1)
	}

	service, err := forms.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		panic(err)
	}

	form, err := service.Forms.Create(&forms.Form{
		Info: &forms.Info{
			Title:         electionConfig.Name + " Application",
			DocumentTitle: electionConfig.Name + " Application",
		},
	}).Do()
	if err != nil {
		panic(err)
	}

	positionOptions := make([]*forms.Option, 0)
	for _, position := range electionConfig.Positions {
		positionOptions = append(positionOptions, &forms.Option{
			Value: position.Name,
		})
	}
	formRequests := []*forms.Request{{
		CreateItem: &forms.CreateItemRequest{
			Item: &forms.Item{
				Title:       "Board positions you'd like to be considered for",
				Description: "Select all positions that you would be willing and able to fulfill. You will be considered for them in the order they appear.",
				QuestionItem: &forms.QuestionItem{
					Question: &forms.Question{
						ChoiceQuestion: &forms.ChoiceQuestion{
							Options: positionOptions,
							Type:    "CHECKBOX",
						},
						Required: true,
					},
				},
			},
			Location: &forms.Location{Index: 0, ForceSendFields: []string{"Index"}},
		},
	}, {
		UpdateFormInfo: &forms.UpdateFormInfoRequest{
			Info: &forms.Info{
				Description:   electionConfig.ApplicationDescription,
				Title:         electionConfig.Name + " Application",
				DocumentTitle: electionConfig.Name + " Application",
			},
			UpdateMask: "*",
		},
	}}

	_, err = service.Forms.BatchUpdate(form.FormId, &forms.BatchUpdateFormRequest{Requests: formRequests}).Do()
	if err != nil {
		panic(err)
	}

	formEditURL := "https://docs.google.com/forms/d/" + form.FormId + "/edit"

	// make the election administrator make some changes
message:
	fmt.Println("")
	fmt.Println("Form URL: " + formEditURL)
	fmt.Println("At this point (since Google is kinda poopy and doesn't have a complete Forms API)\n\t - Turn on 'Collect email addresses'\n\t - Turn on 'Allow response editing'\n\t - Turn on 'Limit to 1 response'\n\t - Link a spreadsheet & make that spreadsheet publicly viewable")
	fmt.Print("When you are done, press Enter:")
	fmt.Scanf("%s\n")

	form, err = service.Forms.Get(form.FormId).Do()
	if err != nil {
		panic(err)
	}

	if form.LinkedSheetId == "" {
		fmt.Println("You forgot to link a spreadsheet! I'll give you another chance.")
		time.Sleep(1 * time.Second)
		goto message
	}
	formViewURL := form.ResponderUri

	sendWebhook("<@&" + fmt.Sprint(discordConfig.RoleID) + "> Candidacy applications for the " + electionConfig.Name + " are now open! Please fill out this form before the deadline: " + formViewURL +
		".\n\n**Make sure you submit the form while logged in to one of the following email addresses:**\n```" + strings.Join(eligibleApplicants, "\n") + "\n```")
	sendWebhook("Application results are updated live at https://docs.google.com/spreadsheets/d/" + form.LinkedSheetId + ".")

	os.Mkdir("state", 0700)
	f, err := os.Create("state/application.txt")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	io.WriteString(f, form.FormId)

	fmt.Println("You're all set!")
}

func main() {
	// flag parsing
	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		fmt.Fprintf(os.Stderr, "usage: %s [ACTION] [OPTIONS...]\n\tpossible actions: start-application, start-vote, end-vote\n", os.Args[0])
		os.Exit(2)
	}
	subcommand := os.Args[1]
	if subcommand != "start-application" && subcommand != "start-vote" && subcommand != "end-vote" {
		fmt.Fprintln(os.Stderr, "invalid action. type "+os.Args[0]+" --help for more information")
		os.Exit(2)
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
			tok, err := config.Exchange(context.Background(), q.Get("code"))
			if err != nil {
				panic(err)
			}
			client := config.Client(context.Background(), tok)

			switch subcommand {
			case "start-application":
				go handle_start_appliction(client)
				io.WriteString(w, "Authorized! Return to your terminal please :)")
			case "start-vote":
				panic("unimplemented")
			case "end-vote":
				panic("unimplemented")
			}
		}
	})

	r.Handle("/auth", http.RedirectHandler(auth_url, http.StatusTemporaryRedirect))

	http.Handle("/", r)
	http.ListenAndServe("localhost:4444", nil)
}
