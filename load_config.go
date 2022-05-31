package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
)

type DiscordConfig struct {
	Webhook string `json:"webhook"`
	RoleID  uint64 `json:"role_id"`
}

type Config struct {
	Name                   string     `json:"name"`
	VoteDescription        string     `json:"vote_description"`
	ApplicationDescription string     `json:"application_description"`
	Positions              []Position `json:"positions"`
}

type Position struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var eligibleApplicants []string
var eligibleVoters []string
var electionConfig Config
var discordConfig DiscordConfig

func init() {
	appBytes, err := ioutil.ReadFile("config/applicants.txt")
	if err != nil {
		panic(err)
	}
	eligibleApplicants = strings.Split(string(appBytes), "\n")
	for i, eligibleApplicant := range eligibleApplicants {
		eligibleApplicants[i] = strings.TrimSpace(eligibleApplicant)
	}

	votersBytes, err := ioutil.ReadFile("config/voters.txt")
	if err != nil {
		panic(err)
	}
	eligibleVoters = strings.Split(string(votersBytes), "\n")
	for i, eligibleVoter := range eligibleVoters {
		eligibleVoters[i] = strings.TrimSpace(eligibleVoter)
	}

	configBytes, err := ioutil.ReadFile("config/positions.json")
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(configBytes, &electionConfig)
	if err != nil {
		panic(err)
	}

	discordBytes, err := ioutil.ReadFile("config/discord.json")
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(discordBytes, &discordConfig)
	if err != nil {
		panic(err)
	}
}

func sendWebhook(text string) {
	req, err := json.Marshal(map[string]interface{}{
		"username": "Election Bot",
		"content":  text,
	})
	if err != nil {
		panic(err)
	}
	http.Post(discordConfig.Webhook, "application/json", bytes.NewReader(req))
}
