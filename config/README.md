This folder contains the files that you will have to modify before starting the process.

Specifically, it contains:
 - `voters.txt` - a list of email addresses for eligible voters, one email address per line. Only people listed
    in this document will be able to vote.
 - `applicants.txt` - a list of email addresses, for people that are eligible to run for election
 - `positions.json` - names + descriptions of election positions, as well as overall description
 - `discord.json` - a JSON object with fields:
    - `webhook`, string - discord webhook URL
    - `role_id`, number - the ID of the Robotics role
    - `board_id`, number - the ID of the Robotics Board role