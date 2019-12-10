package contact

import "fmt"
import "os"
import "github.com/aws/aws-sdk-go/aws"
import "github.com/aws/aws-sdk-go/aws/session"
import "github.com/aws/aws-sdk-go/service/sns"
import "github.com/aws/aws-sdk-go/service/dynamodb"
import "github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"

func SetupNewUser(c Contact) {
	SendSMSToContact("Welcome!", c)
	c.Save()
}

func AddNewUser(number string) {
	c := Contact{Phone: number}
	c.Save()
	txtMsg := fmt.Sprintf("New user. Added %s to db", c.Phone)
	SendSMSToContact(txtMsg, c)
	return
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

func IsNewContact(c Contact) bool {
	contact := FetchContact(c.Phone)
	if len(contact.Phone) > 0 {
		return false
	}
	return true
}

func FetchContact(ph string) Contact {
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

func (c Contact) CheckForEmptyFields() {
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

func (c Contact) DeleteContact() {
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

func (c Contact) UpdateDir(d string) {
	c.Dir = d
	c.Save()
}

func (c Contact) UpdateLine(l string) {
	c.Line = l
	c.Save()
}

func (c Contact) UpdateStation(s string) {
	c.Station = s
	c.Save()
}

func (c Contact) SendStations() {
	msg := "12th 16th 19th 24th ashb antc balb " +
		"bayf cast civc cols colm conc daly " +
		"dbrk dubl deln plza embr frmt ftvl " +
		"glen hayw lafy lake mcar mlbr mont " +
		"nbrk ncon oakl orin pitt pctr phil " +
		"powl rich rock sbrn sfia sanl shay " +
		"ssan ucty warm wcrk wdub woak"
	SendSMSToContact(msg, c)
}

func (c Contact) ProvideConfig() {
	contact := FetchContact(c.Phone)
	alertTxt := fmt.Sprintf("Settings\n\nStation: %s\nDir: %s\nLine: %s", contact.Station, contact.Dir, contact.Line)
	SendSMSToContact(alertTxt, contact)
}

func (c Contact) SendHelp() {
	contact := FetchContact(c.Phone)
	alertTxt := "Stations: mont, powl, ncon (!stations for list)\nDir: n, s\nLine: yellow, red, blue, orange, green\n\ncommands:\n!help - this command\ndeleteme - remove record\nwhoami - show config\nready - get train info"
	SendSMSToContact(alertTxt, contact)
}

func (c Contact) Save() {
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

type Contact struct {
	Phone   string
	Dir     string
	Station string
	Line    string
}
