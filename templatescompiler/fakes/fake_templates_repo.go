package fakes

import (
	"fmt"

	"github.com/cloudfoundry-incubator/candiedyaml"

	bosherr "github.com/cloudfoundry/bosh-agent/errors"

	bmreljob "github.com/cloudfoundry/bosh-micro-cli/release/jobs"
	bmtempcomp "github.com/cloudfoundry/bosh-micro-cli/templatescompiler"
)

type SaveInput struct {
	Job    bmreljob.Job
	Record bmtempcomp.TemplateRecord
}

type saveOutput struct {
	err error
}

type FindInput struct {
	Job bmreljob.Job
}

type findOutput struct {
	record bmtempcomp.TemplateRecord
	found  bool
	err    error
}

type FakeTemplatesRepo struct {
	SaveInputs []SaveInput
	FindInputs []FindInput

	saveBehavior map[string]saveOutput
	findBehavior map[string]findOutput
}

func NewFakeTemplatesRepo() *FakeTemplatesRepo {
	return &FakeTemplatesRepo{
		SaveInputs:   []SaveInput{},
		FindInputs:   []FindInput{},
		saveBehavior: map[string]saveOutput{},
		findBehavior: map[string]findOutput{},
	}
}

func (f *FakeTemplatesRepo) Save(job bmreljob.Job, record bmtempcomp.TemplateRecord) error {
	input := SaveInput{Job: job, Record: record}
	f.SaveInputs = append(f.SaveInputs, input)

	inputString, err := marshalToString(input)
	if err != nil {
		return bosherr.WrapError(err, "Marshaling Save input")
	}
	output, found := f.saveBehavior[inputString]

	if found {
		return output.err
	}
	return fmt.Errorf("Unsupported Input: Save('%#v', '%#v')", job, record)
}

func (f *FakeTemplatesRepo) SetSaveBehavior(job bmreljob.Job, record bmtempcomp.TemplateRecord, err error) error {
	input := SaveInput{Job: job, Record: record}
	inputString, marshalErr := marshalToString(input)
	if marshalErr != nil {
		return bosherr.WrapError(marshalErr, "Marshaling Save input")
	}
	f.saveBehavior[inputString] = saveOutput{err: err}
	return nil
}

func (f *FakeTemplatesRepo) Find(job bmreljob.Job) (bmtempcomp.TemplateRecord, bool, error) {
	input := FindInput{Job: job}
	f.FindInputs = append(f.FindInputs, input)

	inputString, err := marshalToString(input)
	if err != nil {
		return bmtempcomp.TemplateRecord{}, false, bosherr.WrapError(err, "Marshaling Find input")
	}
	output, found := f.findBehavior[inputString]

	if found {
		return output.record, output.found, output.err
	}
	return bmtempcomp.TemplateRecord{}, false, fmt.Errorf("Unsupported input: Find('%#v')", job)
}

func (f *FakeTemplatesRepo) SetFindBehavior(job bmreljob.Job, record bmtempcomp.TemplateRecord, found bool, err error) error {
	input := FindInput{Job: job}
	inputString, marshalErr := marshalToString(input)
	if marshalErr != nil {
		return bosherr.WrapError(marshalErr, "Marshaling Find input")
	}
	f.findBehavior[inputString] = findOutput{record: record, found: found, err: err}
	return nil
}

func marshalToString(input interface{}) (string, error) {
	bytes, err := candiedyaml.Marshal(input)
	if err != nil {
		return "", bosherr.WrapError(err, "Marshaling to string: %#v", input)
	}
	return string(bytes), nil
}
