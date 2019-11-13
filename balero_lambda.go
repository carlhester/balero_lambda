package main

import "fmt"
import "context"
import "net/http"
import "io/ioutil"
import "os"
import "strconv"
import "strings"
import "sort"
import "time"
import "encoding/json"
import "github.com/aws/aws-sdk-go/aws"
import "github.com/aws/aws-sdk-go/aws/session"
import "github.com/aws/aws-sdk-go/service/sns"
import "github.com/aws/aws-lambda-go/lambda"
import "github.com/aws/aws-lambda-go/events"
import "github.com/aws/aws-sdk-go/service/dynamodb"
import "github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"

func main() {
	lambda.Start(HandleRequest)
}

func HandleRequest(ctx context.Context, snsEvent events.SNSEvent) {
	const KEY = "MW9S-E7SL-26DU-VV8V" // public use key from bart website
	timeWindow := 15

	for _, record := range snsEvent.Records {

		messageEnvelope := unpackSNSEvent(record)
		c := fetchContact(messageEnvelope.OriginationNumber)

		if len(c.Phone) == 0 {
			addNewUser(messageEnvelope.OriginationNumber)
			c := fetchContact(messageEnvelope.OriginationNumber)
			sendHelp(c)
			return
		}

		msg := strings.ToLower(messageEnvelope.Body)

		switch msg {
		case "!help":
			sendHelp(c)
			return

		case "12th", "16th", "19th", "24th", "ashb", "antc", "balb",
			"bayf", "cast", "civc", "cols", "colm", "conc", "daly",
			"dbrk", "dubl", "deln", "plza", "embr", "frmt", "ftvl",
			"glen", "hayw", "lafy", "lake", "mcar", "mlbr", "mont",
			"nbrk", "ncon", "oakl", "orin", "pitt", "pctr", "phil",
			"powl", "rich", "rock", "sbrn", "sfia", "sanl", "shay",
			"ssan", "ucty", "warm", "wcrk", "wdub", "woak":
			c.updateStation(msg)
			c.provideConfig()
			return

		case "n", "s":
			c.updateDir(msg)
			c.provideConfig()
			return

		case "yellow", "red", "blue", "orange", "green":
			c.updateLine(msg)
			c.provideConfig()
			return

		case "whoami":
			c.provideConfig()
			return

		case "deleteme":
			c.deleteContact()
			return
		}

		checkForEmptyFields(c)

		if !(msg == "ready") {
			sendHelp(c)
			return
		}

		ackTxt := fmt.Sprintf("here are the next three %s line trains heading %s from %s within %d minutes of each other.\n",
			strings.ToLower(c.Line), strings.ToLower(c.Dir), strings.ToLower(c.Station), timeWindow)
		SendSMSToContact(ackTxt, c)

		url := prepareUrl(c.Station, KEY, c.Dir)
		rawData := rawDataFromUrl(url)
		usableData := RawDataIntoDataStruct(rawData)

		targetTrains, targetMinutes := buildTargets(*usableData, c)

		timeStamp := fetchTimestamp()

		intMin := convertStrMinutesToInt(targetMinutes)
		sort.Ints(intMin)

		alertMsg := timeStamp
		numResults := 0

		if len(intMin) > 2 {
			for i, _ := range intMin[:len(intMin)-2] {
				twoTrainDelta := intMin[i+2] - intMin[i]
				if twoTrainDelta <= timeWindow {
					partAlertMsg := fmt.Sprintf("%s %d \n%s %d \n%s %d\n%d", targetTrains[i], intMin[i], targetTrains[i+1], intMin[i+1], targetTrains[i+2], intMin[i+2], twoTrainDelta)
					alertMsg = fmt.Sprintf("%s\n%s\n", alertMsg, partAlertMsg)
					numResults += 1
				}
			}
		}

		if numResults > 0 {
			SendSMSToContact(alertMsg, c)
		} else {
			SendSMSToContact("No trains found", c)
		}

	}
	return
}

func checkForEmptyFields(c Contact) {
	if len(c.Station) == 0 {
		SendSMSToContact("No station on your profile. Please provide a station abbreviation.", c)
		return
	}

	if len(c.Line) == 0 {
		SendSMSToContact("No line on your profile. Please provide a line (color).", c)
		return
	}

	if len(c.Dir) == 0 {
		SendSMSToContact("No direction on your profile. Please provide a direction.", c)
		return
	}
}

func buildTargets(usableData Data, c Contact) ([]string, []string) {
	var targetTrains []string
	var targetMinutes []string
	for _, train := range usableData.Root.Station[0].Etd {
		for _, est := range train.Est {
			if strings.EqualFold(est.Color, c.Line) {
				targetTrains = append(targetTrains, train.Abbreviation)
				targetMinutes = append(targetMinutes, est.Minutes)
			}
		}
	}
	return targetTrains, targetMinutes
}

func addNewUser(number string) {
	c := Contact{Phone: number}
	c.save()
	txtMsg := fmt.Sprintf("New user. Added %s to db", c.Phone)
	SendSMSToContact(txtMsg, c)
	return
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

func setupNewUser(c Contact) {
	SendSMSToContact("Welcome!", c)
	c.save()
}

func (c Contact) provideConfig() {
	contact := fetchContact(c.Phone)
	alertTxt := fmt.Sprintf("Station: %s\nDir: %s\nLine: %s", contact.Station, contact.Dir, contact.Line)
	SendSMSToContact(alertTxt, contact)
}

func sendHelp(c Contact) {
	contact := fetchContact(c.Phone)
	alertTxt := "Stations: mont, powl, ncon\nDir: n, s\nLine: yellow, red, blue, orange, green\n\ncommands:\n!help - this command\ndeleteme - remove record\nwhoami - show config\nready - get train info"
	SendSMSToContact(alertTxt, contact)
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

func convertStrMinutesToInt(minutes []string) []int {
	var intMin []int
	for _, strMin := range minutes {
		if strMin == "Leaving" {
			strMin = "0"
		}
		i, err := strconv.Atoi(strMin)
		if err != nil {
			panic(err.Error())
		}
		intMin = append(intMin, i)
	}
	return intMin
}

func SendSMSToContact(message string, contact Contact) {
	sess := session.Must(session.NewSession())
	svc := sns.New(sess)

	params := &sns.PublishInput{
		Message:     aws.String(message),
		PhoneNumber: aws.String(contact.Phone),
	}
	_, err := svc.Publish(params)

	if err != nil {
		fmt.Println(err.Error())
		return
	}
}

func RawDataIntoDataStruct(rawData []byte) *Data {
	var usableData Data
	json.Unmarshal([]byte(rawData), &usableData)
	return &usableData
}

func isNewContact(c Contact) bool {
	contact := fetchContact(c.Phone)
	if len(contact.Phone) > 0 {
		return false
	}
	return true
}

func fetchContact(ph string) Contact {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := dynamodb.New(sess)
	result, err := svc.GetItem(&dynamodb.GetItemInput{
		TableName: aws.String("db_test"),
		Key: map[string]*dynamodb.AttributeValue{
			"Phone": {
				S: aws.String(ph),
			},
		},
	})

	if err != nil {
		fmt.Println(err.Error())
	}

	contact := Contact{}
	err = dynamodbattribute.UnmarshalMap(result.Item, &contact)
	if err != nil {
		fmt.Errorf("failed to unmarshal Query result items, %v", err)
	}

	return contact
}

func (c Contact) deleteContact() {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := dynamodb.New(sess)

	input := &dynamodb.DeleteItemInput{
		Key: map[string]*dynamodb.AttributeValue{
			"Phone": {
				S: aws.String(c.Phone),
			},
		},
		TableName: aws.String("db_test"),
	}

	_, err := svc.DeleteItem(input)

	if err != nil {
		fmt.Printf("failed to delete result items, %v", err)
	} else {
		confirmation := fmt.Sprintf("Deleted %s", c.Phone)
		SendSMSToContact(confirmation, c)
	}
}

func (c Contact) updateDir(d string) {
	c.Dir = d
	c.save()
}

func (c Contact) updateLine(l string) {
	c.Line = l
	c.save()
}

func (c Contact) updateStation(s string) {
	c.Station = s
	c.save()
}

func (c Contact) save() {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	svc := dynamodb.New(sess)

	updContact := Contact{
		Phone:   c.Phone,
		Dir:     c.Dir,
		Station: c.Station,
		Line:    c.Line,
	}

	av, err := dynamodbattribute.MarshalMap(updContact)

	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	input := &dynamodb.PutItemInput{
		Item:      av,
		TableName: aws.String("db_test"),
	}

	_, err = svc.PutItem(input)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

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

type Contact struct {
	Phone   string
	Dir     string
	Station string
	Line    string
}
