package httpapi

import "fmt"

// StepMsgid is the msgid of a job title or step name filled with one argument: a
// step is one string on the wire ("Check nginx"), so the UI finds its msgid by
// matching each of StepMsgids with %s as (.+), in order, and translates that msgid
// with the matched text.
type StepMsgid string

const (
	StepSaveConfigMsgid StepMsgid = "Save %s configuration"
	StepInstallPHPMsgid StepMsgid = "Install PHP %s"
	StepRemovePHPMsgid  StepMsgid = "Remove PHP %s"
	StepSaveMsgid       StepMsgid = "Save %s"
	StepCheckMsgid      StepMsgid = "Check %s"
	StepStartMsgid      StepMsgid = "Start %s"
	StepStopMsgid       StepMsgid = "Stop %s"
	StepRestartMsgid    StepMsgid = "Restart %s"
)

// StepMsgids lists every StepMsgid, the most specific first: "Save %s configuration"
// before "Save %s". A fixed step name that one of them also matches ("Restart
// server") is its own msgid, so the UI tries the step as a msgid first.
var StepMsgids = []StepMsgid{
	StepSaveConfigMsgid,
	StepInstallPHPMsgid,
	StepRemovePHPMsgid,
	StepSaveMsgid,
	StepCheckMsgid,
	StepStartMsgid,
	StepStopMsgid,
	StepRestartMsgid,
}

// stepMsgidsTS is StepMsgids for the UI: tygo skips vars, so it emits this text into
// types.gen.ts. TestStepMsgidsReachTheUIInOrder keeps it in step with StepMsgids.
//
//tygo:emit
var stepMsgidsTS = `export const StepMsgids: StepMsgid[] = [
  "Save %s configuration",
  "Install PHP %s",
  "Remove PHP %s",
  "Save %s",
  "Check %s",
  "Start %s",
  "Stop %s",
  "Restart %s",
];
`

// stepName fills a StepMsgid with its argument, as the wire carries it.
func stepName(m StepMsgid, arg string) string {
	return fmt.Sprintf(string(m), arg)
}
