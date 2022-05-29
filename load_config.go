package main

import (
	"io/ioutil"
	"strings"
)

var eligibleApplicants []string
var eligibleVoters []string

func init() {
	app_bytes, err := ioutil.ReadFile("config/applicants.txt")
	if err != nil {
		panic(err)
	}
	eligibleApplicants = strings.Split(string(app_bytes), "\n")
	for i, eligibleApplicant := range eligibleApplicants {
		eligibleApplicants[i] = strings.TrimSpace(eligibleApplicant)
	}

	voters_bytes, err := ioutil.ReadFile("config/voters.txt")
	if err != nil {
		panic(err)
	}
	eligibleVoters := strings.Split(string(voters_bytes), "\n")
	for i, eligibleVoter := range eligibleVoters {

	}
}
