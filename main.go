package main

import . "balero_lambda/contact"
import "fmt"
import "context"
import "net/http"
import "io/ioutil"
import "strconv"
import "strings"
import "sort"
import "time"
import "encoding/json"
import "github.com/aws/aws-lambda-go/lambda"
import "github.com/aws/aws-lambda-go/events"

func main() {
	lambda.Start(HandleRequest)
}

func HandleRequest(ctx context.Context, snsEvent events.SNSEvent) {
	const KEY = "MW9S-E7SL-26DU-VV8V" // public use key from bart website

	for _, record := range snsEvent.Records {

		messageEnvelope := unpackSNSEvent(record)

		var contact Contact = FetchContact(messageEnvelope.OriginationNumber)

		if len(contact.Phone) == 0 {
			AddNewUser(messageEnvelope.OriginationNumber)
			contact = FetchContact(messageEnvelope.OriginationNumber)
			contact.SendHelp()
			return
		}

		msg := strings.ToLower(messageEnvelope.Body)

		switch msg {
		case "!help":
			contact.SendHelp()
			return

		case "!stations":
			contact.SendStations()
			return

		case "12th", "16th", "19th", "24th", "ashb", "antc", "balb",
			"bayf", "cast", "civc", "cols", "colm", "conc", "daly",
			"dbrk", "dubl", "deln", "plza", "embr", "frmt", "ftvl",
			"glen", "hayw", "lafy", "lake", "mcar", "mlbr", "mont",
			"nbrk", "ncon", "oakl", "orin", "pitt", "pctr", "phil",
			"powl", "rich", "rock", "sbrn", "sfia", "sanl", "shay",
			"ssan", "ucty", "warm", "wcrk", "wdub", "woak":
			contact.UpdateStation(msg)
			contact.ProvideConfig()
			return

		case "n", "s":
			contact.UpdateDir(msg)
			contact.ProvideConfig()
			return

		case "yellow", "red", "blue", "orange", "green":
			contact.UpdateLine(msg)
			contact.ProvideConfig()
			return

		case "whoami":
			contact.ProvideConfig()
			return

		case "deleteme":
			contact.DeleteContact()
			return
		}

		contact.CheckForEmptyFields()

		if !(msg == "ready") {
			contact.SendHelp()
			return
		}

		url := prepareUrl(contact.Station, KEY, contact.Dir)
		rawData := rawDataFromUrl(url)
		usableData := RawDataIntoDataStruct(rawData)

		// targetTrains is a slice of TargetTrain structs sorted by minutes
		targetTrains := buildTargets(*usableData, contact)

		targetTrains = scoreTargets(targetTrains, contact)

		// set up the message we'll Send back to user
		timeStamp := fetchTimestamp()
		alertMsg := timeStamp
		numResults := 0

		for _, train := range targetTrains {
			numResults += 1
			if train.Score > 0 {
				partAlertMsg := fmt.Sprintf("%d pts - %s in %d minutes", train.Score, train.TrainName, train.Minutes)
				alertMsg = fmt.Sprintf("%s\n%s", alertMsg, partAlertMsg)
			}
		}

		if numResults > 0 {
			SendSMSToContact(alertMsg, contact)
		} else {
			SendSMSToContact("No trains found", contact)
		}

	}
	return
}

func buildTargets(usableData Data, c Contact) []TargetTrain {
	targets := []TargetTrain{}
	for _, train := range usableData.Root.Station[0].Etd {
		for _, est := range train.Est {
			//if strings.EqualFold(est.Color, c.Line) {
			var i TargetTrain
			i.TrainName = train.Abbreviation
			i.Minutes = convertStrMinutesToInt(est.Minutes)
			i.Line = est.Color
			i.Score = 0
			targets = append(targets, i)
			//}
		}
	}
	targets = sortSliceOfTargetTrains(targets)
	return targets
}

func scoreTargets(targets []TargetTrain, c Contact) []TargetTrain {
	targetLineTrains := []TargetTrain{}

	for i, train := range targets {

		switch train.TrainName {
		// if train going to my stop add 2
		case "WCRK":
			targets[i].Score += 2

		// if train going past my stop (NCON, ANTC, PHIL, PITT) add 1
		case "NCON", "ANTC", "PHIL", "PITT":
			targets[i].Score += 1
		}

		// if previous train was < 3 minutes ago + 5
		if i > 0 {
			if (targets[i].Minutes - targets[i-1].Minutes) < 5 {
				targets[i].Score += 5
			}
		}

		// if 2 previous trains on any line were within < 15 minutes ago + 10
		if i > 1 {
			if (targets[i].Minutes - targets[i-2].Minutes) < 15 {
				targets[i].Score += 10
			}
		}
		// if train on my line, consider it a candidate and give it a point
		if strings.EqualFold(c.Line, targets[i].Line) {
			targetLineTrains = append(targetLineTrains, train)
			targets[i].Score += 1
		} else {
			//if the train isn't on my line, set its score to zero
			targets[i].Score = 0
		}

	}

	// loop over trains on my line
	// if 3 trains on my line are within 15 minutes, give the third one 20 pts
	for j, _ := range targetLineTrains {
		if j > 1 {
			if targetLineTrains[j].Minutes-targetLineTrains[j-2].Minutes < 15 {
				for i, _ := range targets {
					if targets[i].TrainName == targetLineTrains[j].TrainName {
						if targets[i].Minutes == targetLineTrains[j].Minutes {
							targets[i].Score += 20
						}
					}
				}

			}
		}
	}

	return targets
}

func unpackSNSEvent(record events.SNSEventRecord) SNSMessage {
	snsRecord := record.SNS
	message := SNSMessage{}
	_ = json.Unmarshal([]byte(snsRecord.Message), &message)
	return message
}

func fetchTimestamp() string {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		panic(err.Error())
	}
	currTime := time.Now()
	currTime = currTime.In(loc)
	timeStamp := fmt.Sprintf("%s", currTime.Format("Jan _2 15:04:05"))
	return timeStamp
}

func rawDataFromUrl(url string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		panic(err.Error())
	}
	data, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()

	if err != nil {
		panic(err.Error())
	}
	return data
}

func prepareUrl(station string, key string, dir string) string {
	url := "http://api.bart.gov/api/etd.aspx?cmd=etd&orig=" + station + "&key=" + key + "&dir=" + dir + "&json=y"
	return url
}

func sortSliceOfTargetTrains(targets []TargetTrain) []TargetTrain {
	sort.Slice(targets, func(i, j int) bool { return targets[i].Minutes < targets[j].Minutes })
	return targets
}

func convertStrMinutesToInt(minutes string) int {
	if minutes == "Leaving" {
		minutes = "0"
	}
	i, err := strconv.Atoi(minutes)
	if err != nil {
		panic(err.Error())
	}
	return i
}

func RawDataIntoDataStruct(rawData []byte) *Data {
	var usableData Data
	json.Unmarshal([]byte(rawData), &usableData)
	return &usableData
}

type Estimates []struct {
	Minutes     string `json:"minutes"`
	Direction   string `json:"direction"`
	Length      int    `json:"length"`
	Color       string `json:"color"`
	Hexcolor    string `json:"hexcolor"`
	Bikeflag    int    `json:"bikeflag"`
	Delay       int    `json:"delay"`
	Carflag     int    `json:"carflag"`
	Cancelflag  int    `json:"cancelflag"`
	Dynamicflag int    `json:"dynamicflag"`
}

type Etd []struct {
	Destination  string    `json:"destination"`
	Abbreviation string    `json:"abbreviation"`
	Limited      int       `json:"limited"`
	Est          Estimates `json:"estimate"`
}

type Station []struct {
	Name string `json:"name"`
	Abbr string `json:"abbr"`
	Etd  Etd    `json:"etd"`
}

type Uri struct {
	Cdata string `json:"#cdata-section"`
}

type Root struct {
	Id      int     `json:"@id"`
	Uri     Uri     `json:"uri"`
	Date    string  `json:"date"`
	Time    string  `json:"time"`
	Station Station `json:"station"`
	Message string  `json:"message"`
}

type Xml struct {
	Version  string `json:"@version"`
	Encoding string `json:"@encoding"`
}

type Data struct {
	Xml  Xml  `json:"?xml"`
	Root Root `json:"root"`
}

type SNSMessage struct {
	OriginationNumber          string `json:"originationNumber"`
	DestinationNumber          string `json:"DestinationNumber"`
	MessageKeyword             string `json:"messageKeyword"`
	Body                       string `json:"messageBody"`
	InboundMessageId           string `json:"inboundMessageId"`
	PreviousPublishedMessageId string `json:"previousPublishedMessageId"`
}

type TargetTrain struct {
	TrainName string
	Line      string
	Minutes   int
	Score     int
}
