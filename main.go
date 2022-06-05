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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/forms/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type Credentials struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
}

func handle_start_vote(client *http.Client) {
	if _, err := os.Stat("state/ballot.txt"); err == nil {
		fmt.Println("`state/ballot.txt` already exists, meaning voting has already started!")
		os.Exit(1)
	}

	var applicationID string
	{
		applicationIdBytes, err := os.ReadFile("state/application.txt")
		if err != nil {
			fmt.Println("`state/application.txt' does not exist, meaning you haven't opened applications! To open applications, use the start-application subcommand.")
			os.Exit(1)
		}
		applicationID = string(applicationIdBytes)
	}

	service, err := forms.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		panic(err)
	}

	nameQuestionID := ""
	positionsQuestionID := ""
	{
		applicationForm, err := service.Forms.Get(applicationID).Do()
		if err != nil {
			panic(err)
		}
		for _, item := range applicationForm.Items {
			if item.Title == "Name" && item.QuestionItem != nil && item.QuestionItem.Question != nil && item.QuestionItem.Question.TextQuestion != nil {
				nameQuestionID = item.QuestionItem.Question.QuestionId
			}
			if item.Title == "Positions" && item.QuestionItem != nil && item.QuestionItem.Question != nil && item.QuestionItem.Question.ChoiceQuestion != nil {
				positionsQuestionID = item.QuestionItem.Question.QuestionId
			}
		}

		if nameQuestionID == "" || positionsQuestionID == "" {
			missing := ""
			if nameQuestionID == "" {
				missing += "Name "
			}
			if positionsQuestionID == "" {
				missing += "Positions "
			}
			fmt.Println("Invalid application form; missing questions: " + missing)
			os.Exit(1)
		}
	}

	applicantsByPosition := make(map[string][]string)
	// {email, name} tuple
	ineligibleApplicants := [][2]string{}
	{
		applicantResponses, err := service.Forms.Responses.List(applicationID).Do()
		if err != nil {
			panic(err)
		}
		if applicantResponses.NextPageToken != "" {
			fmt.Println("There are more than " + fmt.Sprint(len(applicantResponses.Responses)) + " responses! This program cannot process more than 5000 responses.")
			os.Exit(1)
		}
		for _, resp := range applicantResponses.Responses {
			isEligibleApplicant := false
			for _, eligibleApplicant := range eligibleApplicants {
				if strings.EqualFold(eligibleApplicant, resp.RespondentEmail) {
					isEligibleApplicant = true
				}
			}

			applicantName := resp.Answers[nameQuestionID].TextAnswers.Answers[0].Value
			applicantPositions := []string{}
			for _, textAnswer := range resp.Answers[positionsQuestionID].TextAnswers.Answers {
				applicantPositions = append(applicantPositions, textAnswer.Value)
			}

			if !isEligibleApplicant {
				ineligibleApplicants = append(ineligibleApplicants, [2]string{strings.ToLower(resp.RespondentEmail), applicantName})
			} else {
				for _, position := range applicantPositions {
					applicantsByPosition[position] = append(applicantsByPosition[position], applicantName)
				}
			}
		}
	}

	if len(ineligibleApplicants) != 0 {
		fmt.Println("Ineligible Applicants:")
		for _, tuple := range ineligibleApplicants {
			fmt.Println("\t- " + tuple[1] + " <" + tuple[0] + ">")
		}
		fmt.Print("Press [Enter] to ignore these applicants, or [Ctrl-C] to address this issue and re-run this command again later: ")
		fmt.Scanln()
		fmt.Println()
	}

	// construct form
	form, err := service.Forms.Create(&forms.Form{
		Info: &forms.Info{
			Title:         electionConfig.Name + " Ballot",
			DocumentTitle: electionConfig.Name + " Ballot",
		},
	}).Do()
	if err != nil {
		panic(err)
	}

	requests := []*forms.Request{}
	for positionIdx, position := range electionConfig.Positions {
		rows := []*forms.Question{}
		for _, applicant := range applicantsByPosition[position.Name] {
			rows = append(rows, &forms.Question{
				RowQuestion: &forms.RowQuestion{
					Title: applicant,
				},
			})
		}
		requests = append(requests, &forms.Request{
			CreateItem: &forms.CreateItemRequest{
				Item: &forms.Item{
					Title:       position.Name,
					Description: position.Description + " \n\nScore each candidate from 0-2, with 2 expressing approval and 0 expressing disapproval. You do not have to fill in every row; blank rows will be treated like a 0.",
					QuestionGroupItem: &forms.QuestionGroupItem{
						Grid: &forms.Grid{
							Columns: &forms.ChoiceQuestion{
								Options: []*forms.Option{
									{Value: "0"},
									{Value: "1"},
									{Value: "2"},
								},
								Type: "RADIO",
							},
						},
						Questions: rows,
					},
				},
				Location: &forms.Location{Index: int64(positionIdx), ForceSendFields: []string{"Index"}},
			},
		})
	}

	scoreDescription := "This election uses score voting. During the voting process, each voter scores each candidate from 0-2 based on how suited to the position the voter thinks the candidate is. " +
		"After votes are in, scores are added up and whichever candidate has the most points is elected. If there is a tie between two or more candidates, there will be a runoff election for that position.\n\n" +
		"To maximize the value of your vote, it is recommended to score 2 for at least one candidate per position."

	requests = append(requests, &forms.Request{
		UpdateFormInfo: &forms.UpdateFormInfoRequest{
			Info: &forms.Info{
				Description: electionConfig.VoteDescription + "\n\n" + scoreDescription,
			},
			UpdateMask: "description",
		},
	})

	_, err = service.Forms.BatchUpdate(form.FormId, &forms.BatchUpdateFormRequest{Requests: requests}).Do()
	if err != nil {
		panic(err)
	}

	fmt.Println()
	fmt.Println("Ballot Form URL: " + "https://docs.google.com/forms/d/" + form.FormId)
	fmt.Println("At this point, do the following on ballot form:")
	fmt.Println("\t- Turn on 'Collect email addresses'")
	fmt.Println("\t- Turn on 'Allow response editing'")
	fmt.Println("\t- Turn on 'Limit to 1 response'")
	fmt.Println("Also:")
	fmt.Println("\t- Close the application form")
	fmt.Print("Press [Enter] when you are done with the above: ")
	fmt.Scanln()
	fmt.Println()
	fmt.Println("As a reminder, do NOT share the raw results with anyone, as this will compromise the anonymity of the voting process.")
	fmt.Print("Press [Enter] to confirm: ")
	fmt.Scanln()

	f, err := os.Create("state/ballot.txt")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	io.WriteString(f, form.FormId)

	sendWebhook(
		"<@&" + fmt.Sprint(discordConfig.RoleID) + "> Voting for the " + electionConfig.Name + " has begun! Fill out this form before the deadline to have your vote counted: " + form.ResponderUri + "\n\n" +
			"All votes are **anonymous**, so please vote for people that you feel are well suited for the position.\nTo maximize the value of your vote, it is recommended to **score 2 for at least one candidate per position**.\nYou may edit your vote anytime before the deadline.\n\n" +
			"Make sure you enter one of the following addresses into the \"Email\" field. **Entering an unlisted email may result in your vote being uncounted.**\n```\n" + strings.Join(eligibleVoters, "\n") + "\n```",
	)
	sendWebhook("BTW: Remember that your election opponents, like a match opponent, may (will) be your alliance partner (team member).")

	fmt.Println("You're all set!")
	os.Exit(0)
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
				Title:       "Name",
				Description: "Full name please.",
				QuestionItem: &forms.QuestionItem{
					Question: &forms.Question{
						TextQuestion:    &forms.TextQuestion{},
						Required:        true,
						ForceSendFields: []string{"TextQuestion"},
					},
				},
			},
			Location: &forms.Location{Index: 0, ForceSendFields: []string{"Index"}},
		},
	}, {
		CreateItem: &forms.CreateItemRequest{
			Item: &forms.Item{
				Title:       "Positions",
				Description: "Select all positions that you would be willing and able to fulfill. You may select multiple. You will be considered for them in the order they appear.",
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
			Location: &forms.Location{Index: 1, ForceSendFields: []string{"Index"}},
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
	fmt.Print("When you are done, press [Enter]: ")
	fmt.Scanln()

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

	sendWebhook("<@&" + fmt.Sprint(discordConfig.RoleID) + "> Candidacy applications for the " + electionConfig.Name + " are now open! Please fill out this form before the deadline: " + formViewURL + ". Before you apply, keep in mind the requirements of the board position that you are applying for.\n\n" +
		"This form can be edited anytime before the application deadline.\n\n" +
		"As a reminder, **bribery and extortion are grounds for your candidacy eligibility to be revoked**. This means no personal promises, goods, money, services, etc in exchange for votes or even an implication of exchange for votes.\n\n" +
		"Make sure you enter one of the following email addresses into the \"Email\" field. **Entering an unlisted email may result in your candidacy not being registered.**\n```" + strings.Join(eligibleApplicants, "\n") + "\n```")
	sendWebhook("Application results are updated live at https://docs.google.com/spreadsheets/d/" + form.LinkedSheetId + ".")

	os.Mkdir("state", 0700)
	f, err := os.Create("state/application.txt")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	io.WriteString(f, form.FormId)

	fmt.Println("You're all set!")
	os.Exit(0)
}

func handleEndVote(client *http.Client) {
	// sanity check
	if _, err := os.Stat("state/results.txt"); err == nil {
		fmt.Println("`state/results.txt' already exists, meaning you've already sent out the results! To restart the election, delete the state folder.")
		os.Exit(1)
	}

	var ballotID string
	{
		ballotIDBytes, err := os.ReadFile("state/ballot.txt")
		if err != nil {
			fmt.Println("`state/ballot.txt' does not exist, meaning you haven't started the vote! Use the `start-vote' command to open the ballot.")
			os.Exit(1)
		}
		ballotID = string(ballotIDBytes)
	}

	service, err := forms.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		panic(err)
	}

	ballot, err := service.Forms.Get(ballotID).Do()
	if err != nil {
		panic(err)
	}

	// question id => {position, candidate}
	questionIDs := make(map[string][2]string)
	for _, item := range ballot.Items {
		if item.QuestionGroupItem != nil && item.QuestionGroupItem.Grid != nil && len(item.QuestionGroupItem.Questions) != 0 {
			for _, row := range item.QuestionGroupItem.Questions {
				questionIDs[row.QuestionId] = [2]string{item.Title, row.RowQuestion.Title}
			}
		}
	}

	// position => candidate => score
	totalScores := make(map[string]map[string]uint)
	responses, err := service.Forms.Responses.List(ballotID).Do()
	if err != nil {
		panic(err)
	}
	ineligibleVoters := []string{}
	numberEligibleVoters := uint(0)
	for _, resp := range responses.Responses {
		isEligible := false
		for _, email := range eligibleVoters {
			if strings.EqualFold(resp.RespondentEmail, email) {
				isEligible = true
			}
		}

		if !isEligible {
			ineligibleVoters = append(ineligibleVoters, strings.ToLower(resp.RespondentEmail))
			continue
		}
		numberEligibleVoters += 1

		for questionID, answer := range resp.Answers {
			tuple := questionIDs[questionID]
			position := tuple[0]
			candidate := tuple[1]
			score, err := strconv.ParseUint(answer.TextAnswers.Answers[0].Value, 10, 32)
			if err != nil {
				panic(err)
			}
			if totalScores[position] == nil {
				totalScores[position] = make(map[string]uint)
			}
			totalScores[position][candidate] += uint(score)
		}
	}

	if len(ineligibleVoters) != 0 {
		fmt.Println("Ineligible voters that voted:")
		for _, voter := range ineligibleVoters {
			fmt.Println("\t- " + voter)
		}
		fmt.Print("Press [Enter] to ignore these votes, or [Ctrl-C] to fix the issue and re-run this command later: ")
		fmt.Scanln()
		fmt.Println()
	}

	sheetsService, err := sheets.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		panic(err)
	}

	rowData := []*sheets.RowData{}
	colData := []*sheets.DimensionProperties{}

	winners := make(map[string]string)
	tie := ""
	tiers := []string{}
	for positionIdx, position := range electionConfig.Positions {
		colData = append(colData, &sheets.DimensionProperties{PixelSize: 192})
		colData = append(colData, &sheets.DimensionProperties{PixelSize: 32})

		type candidateTuple struct {
			name  string
			score uint
		}
		candidates := []candidateTuple{}
		for candidate, score := range totalScores[position.Name] {
			candidates = append(candidates, candidateTuple{name: candidate, score: score})
		}

		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].score > candidates[j].score
		})

		// winners
		if tie == "" {
			for i, candidate := range candidates {
				alreadyWon := false
				for _, winner := range winners {
					if winner == candidate.name {
						alreadyWon = true
					}
				}
				if alreadyWon {
					continue
				}

				if i+1 < len(candidates) && candidates[i+1].score == candidates[i].score {
					tie = position.Name
					for j := i; j < len(candidates); j++ {
						if candidates[j].score == candidate.score {
							tiers = append(tiers, candidates[j].name)
						}
					}
					break
				} else {
					winners[position.Name] = candidate.name
					break
				}
			}
		}

		// configure spreadsheet
		for len(rowData) < len(candidates)+1 {
			rowData = append(rowData, &sheets.RowData{})
		}

		for _, row := range rowData {
			for len(row.Values) < len(electionConfig.Positions)*2 {
				row.Values = append(row.Values, &sheets.CellData{})
			}
		}

		positionName := position.Name
		rowData[0].Values[positionIdx*2].UserEnteredValue = &sheets.ExtendedValue{StringValue: &positionName}
		rowData[0].Values[positionIdx*2].UserEnteredFormat = &sheets.CellFormat{TextFormat: &sheets.TextFormat{Bold: true}}

		for candidateIdx, candidate := range candidates {
			candidateName := candidate.name
			candidateScore := float64(candidate.score)
			rowData[candidateIdx+1].Values[positionIdx*2].UserEnteredValue = &sheets.ExtendedValue{StringValue: &candidateName}
			rowData[candidateIdx+1].Values[positionIdx*2+1].UserEnteredValue = &sheets.ExtendedValue{NumberValue: &candidateScore}
		}
	}

	sheet, err := sheetsService.Spreadsheets.Create(&sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{
			Title: electionConfig.Name + " Results",
		},
		Sheets: []*sheets.Sheet{{
			Properties: &sheets.SheetProperties{Title: "Results", GridProperties: &sheets.GridProperties{
				RowCount:    int64(len(rowData)),
				ColumnCount: int64(len(electionConfig.Positions)) * 2,
			}},
			Data: []*sheets.GridData{{
				RowData:        rowData,
				ColumnMetadata: colData,
			}},
		}},
	}).Do()
	if err != nil {
		panic(err)
	}

	fmt.Println("Ballot Form URL: https://docs.google.com/forms/d/" + ballotID + "/edit#responses")
	fmt.Println("Spreadsheet URL: " + sheet.SpreadsheetUrl)
	fmt.Println("At this point, make sure you do the following:")
	fmt.Println("\t- Close ballot form")
	fmt.Println("\t- Make spreadsheet publicly viewable")
	fmt.Print("When you're done, press [Enter]: ")
	fmt.Scanln()
	fmt.Println()

	f, err := os.Create("state/results.txt")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	io.WriteString(f, sheet.SpreadsheetId)

	embed := &DiscordEmbed{
		Title: electionConfig.Name + " Results",
		Color: 0x88c0d0,
	}

	if tie == "" {
		embed.Description = "Congratulations to our new <@&" + fmt.Sprint(discordConfig.BoardID) + ">!"
		for _, position := range electionConfig.Positions {
			embed.Description += "\n - **" + winners[position.Name] + "** as " + position.Name
		}
	} else {
		embed.Description = "There will be a runoff election for " + tie + " between **" + strings.Join(tiers, "** and **") + "**."
	}

	embed.Fields = append(embed.Fields, &DiscordField{
		Name:   "Results",
		Value:  sheet.SpreadsheetUrl,
		Inline: false,
	})
	embed.Fields = append(embed.Fields, &DiscordField{
		Name:   "Votes",
		Value:  fmt.Sprint(numberEligibleVoters),
		Inline: true,
	})

	if len(ineligibleVoters) != 0 {
		embed.Fields = append(embed.Fields, &DiscordField{
			Name:   "Ineligible Votes",
			Value:  fmt.Sprint(len(ineligibleVoters)),
			Inline: true,
		})
	}

	sendWebhookEmbed("<@&"+fmt.Sprint(discordConfig.RoleID)+"> Results are out! Remember that no matter who wins, "+
		"you're all part of the same team.", embed)

	fmt.Println("As a reminder, DO NOT share the raw results (who voted for who) with anyone, as that would compromise the secrecy of the ballot.")
	fmt.Print("Press [Enter] if you understand: ")
	fmt.Scanln()
	fmt.Println()

	if tie != "" {
		fmt.Println("You're going to need to have a runoff election for " + tie + ". If there are only two candidates, it should be done using FPTP.")
		fmt.Println("This bot will not help you with the runoff election.")
		fmt.Println("After the runoff election, you should be able to deduce the winners based on the results spreadsheet.")
	} else {
		fmt.Println("You're all set! Make sure you update the board roles.")
	}
	os.Exit(0)
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
			"https://www.googleapis.com/auth/forms.responses.readonly",
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
				go handle_start_vote(client)
				io.WriteString(w, "Authorized! Return to your terminal please :)")
			case "end-vote":
				go handleEndVote(client)
				io.WriteString(w, "Authorized! Return to your terminal please :)")
			}
		}
	})

	r.Handle("/auth", http.RedirectHandler(auth_url, http.StatusTemporaryRedirect))

	http.Handle("/", r)
	http.ListenAndServe("localhost:4444", nil)
}
