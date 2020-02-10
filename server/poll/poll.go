package poll

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

var votesSettingPattern = regexp.MustCompile(`^votes=(\d+)$`)

// Poll stores all needed information for a poll
type Poll struct {
	ID            string
	CreatedAt     int64
	Creator       string
	Question      string
	AnswerOptions []*AnswerOption
	Settings      Settings
}

// AnswerOption stores a possible answer and a list of user who voted for this
type AnswerOption struct {
	Answer string
	Voter  []string
}

// Settings stores possible settings for a poll
type Settings struct {
	Anonymous       bool
	Progress        bool
	PublicAddOption bool
	MaxVotes        int
}

// VotedAnswerResponse stores answers that is created by a user
type VotedAnswerResponse struct {
	PollID       string   `json:"poll_id"`
	UserID       string   `json:"user_id"`
	VotedAnswers []string `json:"voted_answers"`
}

// ErrorMessage contains error messsage for a user that can be localized.
// It should not be wrapped and instead always returned.
type ErrorMessage struct {
	Message *i18n.Message
	Data    map[string]interface{}
}

// NewPoll creates a new poll with the given paramatern.
func NewPoll(creator, question string, answerOptions, settings []string) (*Poll, *ErrorMessage) {
	p := Poll{
		ID:        model.NewId(),
		CreatedAt: model.GetMillis(),
		Creator:   creator,
		Question:  question,
		Settings:  Settings{MaxVotes: 1},
	}
	for _, answerOption := range answerOptions {
		if errMsg := p.AddAnswerOption(answerOption); errMsg != nil {
			return nil, errMsg
		}
	}
	for _, s := range settings {
		switch {
		case s == "anonymous":
			p.Settings.Anonymous = true
		case s == "progress":
			p.Settings.Progress = true
		case s == "public-add-option":
			p.Settings.PublicAddOption = true
		case votesSettingPattern.MatchString(s):
			if errMsg := p.ParseVotesSetting(s); errMsg != nil {
				return nil, errMsg
			}
		default:
			return nil, &ErrorMessage{
				Message: &i18n.Message{
					ID:    "poll.newPoll.unrecognizedSetting",
					Other: "Unrecognized poll setting: {{.Setting}}",
				},
				Data: map[string]interface{}{
					"Setting": s,
				},
			}
		}
	}
	return &p, nil
}

// ParseVotesSetting parses and sets a setting for votes ("--votes=X")
func (p *Poll) ParseVotesSetting(s string) *ErrorMessage {
	e := votesSettingPattern.FindStringSubmatch(s)
	if len(e) != 2 {
		return &ErrorMessage{
			Message: &i18n.Message{
				ID:    "poll.newPoll.votesettings.unexpectedError",
				Other: "Unexpected error happens when parsing {{.Setting}}",
			},
			Data: map[string]interface{}{
				"Setting": s,
			},
		}
	}
	i, err := strconv.Atoi(e[1])
	if err != nil || i <= 0 || i > len(p.AnswerOptions) {
		return &ErrorMessage{
			Message: &i18n.Message{
				ID:    "poll.newPoll.votesettings.invalidSetting",
				Other: "In votes=X, X must be a positive number and less than the number of answer options. {{.Setting}}",
			},
			Data: map[string]interface{}{
				"Setting": s,
			},
		}
	}
	p.Settings.MaxVotes = i
	return nil
}

// AddAnswerOption adds a new AnswerOption to a poll
func (p *Poll) AddAnswerOption(newAnswerOption string) *ErrorMessage {
	newAnswerOption = strings.TrimSpace(newAnswerOption)
	if newAnswerOption == "" {
		return &ErrorMessage{
			Message: &i18n.Message{
				ID:    "poll.addAnswerOption.empty",
				Other: "Empty option not allowed",
			},
		}
	}
	for _, answerOption := range p.AnswerOptions {
		if answerOption.Answer == newAnswerOption {
			return &ErrorMessage{
				Message: &i18n.Message{
					ID:    "poll.addAnswerOption.duplicate",
					Other: "Duplicate option: {{.Option}}",
				},
				Data: map[string]interface{}{
					"Option": newAnswerOption,
				},
			}
		}
	}
	p.AnswerOptions = append(p.AnswerOptions, &AnswerOption{Answer: newAnswerOption})
	return nil
}

// UpdateVote performs a vote for a given user
func (p *Poll) UpdateVote(userID string, index int) (*i18n.Message, error) {
	if len(p.AnswerOptions) <= index || index < 0 {
		return nil, fmt.Errorf("invalid index")
	}
	if userID == "" {
		return nil, fmt.Errorf("invalid userID")
	}

	if p.Settings.MaxVotes > 1 {
		// Multi Answer Mode
		votedAnswers, err := p.GetVotedAnswer(userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get existing data")
		}
		for _, answers := range votedAnswers.VotedAnswers {
			if answers == p.AnswerOptions[index].Answer {
				// FIXME: to translated message: "Your already voted for this option."
				// Or ask an user to reset this option
				return &i18n.Message{
					ID:    "poll.updateVote.alreadyVoted",
					Other: "You've already voted for this option.",
				}, nil
			}
		}

		if p.Settings.MaxVotes <= len(votedAnswers.VotedAnswers) {
			// FIXME: to translated message: "You can't vote, because you don't have any votes left. Use the reset button to reset your votes. [" + strings.Join(votedAnswers.VotedAnswers, ", ") + "]"
			// TODO: Enable to display emoji in error message?
			return &i18n.Message{
				ID:    "poll.updateVote.maxVotes",
				Other: "You could't vote for this option, because you don't have any votes left. Use the reset button to reset your votes.",
			}, nil
		}
	} else {
		// Single Answer Mode
		for _, o := range p.AnswerOptions {
			for i := 0; i < len(o.Voter); i++ {
				if userID == o.Voter[i] {
					o.Voter = append(o.Voter[:i], o.Voter[i+1:]...)
				}
			}
		}
	}

	p.AnswerOptions[index].Voter = append(p.AnswerOptions[index].Voter, userID)
	return nil, nil
}

// GetVotedAnswer collect voted answers by a user and returns it as VotedAnswerResponse
func (p *Poll) GetVotedAnswer(userID string) (*VotedAnswerResponse, error) {
	if userID == "" {
		return nil, fmt.Errorf("invalid userID")
	}
	votedAnswer := []string{}
	for _, o := range p.AnswerOptions {
		for _, v := range o.Voter {
			if userID == v {
				votedAnswer = append(votedAnswer, o.Answer)
			}
		}
	}
	return &VotedAnswerResponse{
		PollID:       p.ID,
		UserID:       userID,
		VotedAnswers: votedAnswer,
	}, nil
}

// HasVoted return true if a given user has voted in this poll
func (p *Poll) HasVoted(userID string) bool {
	for _, o := range p.AnswerOptions {
		for i := 0; i < len(o.Voter); i++ {
			if userID == o.Voter[i] {
				return true
			}
		}
	}
	return false
}

// EncodeToByte returns a poll as a byte array
func (p *Poll) EncodeToByte() []byte {
	b, _ := json.Marshal(p)
	return b
}

// DecodePollFromByte tries to create a poll from a byte array
func DecodePollFromByte(b []byte) *Poll {
	p := Poll{}
	err := json.Unmarshal(b, &p)
	if err != nil {
		return nil
	}
	return &p
}

// Copy deep copys a poll
func (p *Poll) Copy() *Poll {
	p2 := new(Poll)
	*p2 = *p
	p2.AnswerOptions = make([]*AnswerOption, len(p.AnswerOptions))
	for i, o := range p.AnswerOptions {
		p2.AnswerOptions[i] = new(AnswerOption)
		p2.AnswerOptions[i].Answer = o.Answer
		p2.AnswerOptions[i].Voter = o.Voter
	}
	return p2
}

// EncodeToByte returns a VotedAnswerResponse as a byte array
func (v *VotedAnswerResponse) EncodeToByte() []byte {
	b, _ := json.Marshal(v)
	return b
}
