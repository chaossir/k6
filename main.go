package main

import (
	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/loadimpact/speedboat/aggregate"
	"github.com/loadimpact/speedboat/loadtest"
	"github.com/loadimpact/speedboat/report"
	"github.com/loadimpact/speedboat/runner"
	"github.com/loadimpact/speedboat/runner/lua"
	"github.com/loadimpact/speedboat/runner/simple"
	"golang.org/x/net/context"
	"io/ioutil"
	"os"
	"path"
	"time"
)

func makeTest(c *cli.Context) (test loadtest.LoadTest, err error) {
	base := ""
	conf := loadtest.NewConfig()
	if len(c.Args()) > 0 {
		filename := c.Args()[0]
		base = path.Dir(filename)
		data, err := ioutil.ReadFile(filename)
		if err != nil {
			return test, err
		}

		loadtest.ParseConfig(data, &conf)
	}

	if c.IsSet("script") {
		conf.Script = c.String("script")
		base = ""
	}
	if c.IsSet("url") {
		conf.URL = c.String("url")
	}
	if c.IsSet("duration") {
		conf.Duration = c.Duration("duration").String()
	}
	if c.IsSet("vus") {
		conf.VUs = c.Int("vus")
	}

	test, err = conf.Compile()
	if err != nil {
		return test, err
	}

	if test.Script != "" {
		srcb, err := ioutil.ReadFile(path.Join(base, test.Script))
		if err != nil {
			return test, err
		}
		test.Source = string(srcb)
	}

	return test, nil
}

func run(test loadtest.LoadTest, r runner.Runner) <-chan runner.Result {
	ch := make(chan runner.Result)

	go func() {
		defer close(ch)

		timeout := time.Duration(0)
		for _, stage := range test.Stages {
			timeout += stage.Duration
		}

		ctx, _ := context.WithTimeout(context.Background(), timeout)
		scale := make(chan int, 1)
		scale <- test.Stages[0].VUs.Start

		for res := range runner.Run(ctx, r, scale) {
			ch <- res
		}
	}()

	return ch
}

func action(c *cli.Context) {
	test, err := makeTest(c)
	if err != nil {
		log.WithError(err).Fatal("Configuration error")
	}

	r := runner.Runner(nil)

	// Start the pipeline by just running requests
	if test.Script != "" {
		r = lua.New(test.Script, test.Source)
	} else {
		r = simple.New(test.URL)
	}
	pipeline := run(test, r)

	// Stick result aggregation onto it
	stats := aggregate.Stats{}
	stats.Time.Values = make([]time.Duration, 30000000)[:0]
	pipeline = aggregate.Aggregate(&stats, pipeline)

	// Log results to a file
	outFilename := c.String("out-file")
	if outFilename != "" {
		reporter := report.CSVReporter{}
		if outFilename != "-" {
			f, err := os.Create("results.csv")
			if err != nil {
				log.WithError(err).Fatal("Couldn't open log file")
			}
			pipeline = report.Report(reporter, f, pipeline)
		} else {
			pipeline = report.Report(reporter, os.Stdout, pipeline)
		}
	}

	for res := range pipeline {
		switch {
		case res.Error != nil:
			l := log.WithError(res.Error)
			if res.Time != time.Duration(0) {
				l = l.WithField("t", res.Time)
			}
			l.Error("Error")
		case res.Text != "":
			l := log.WithField("text", res.Text)
			if res.Time != time.Duration(0) {
				l = l.WithField("t", res.Time)
			}
			l.Info("Log")
		default:
			// log.WithField("t", res.Time).Debug("Metric")
		}
	}

	log.WithField("results", stats.Results).Info("Finished")
	log.WithFields(log.Fields{
		"min": stats.Time.Min,
		"max": stats.Time.Max,
		"med": stats.Time.Med,
		"avg": stats.Time.Avg,
	}).Info("Time")
}

// Configure the global logger.
func configureLogging(c *cli.Context) {
	if c.GlobalBool("verbose") {
		log.SetLevel(log.DebugLevel)
	}
}

func main() {
	// Free up -v and -h for our own flags
	cli.VersionFlag.Name = "version"
	cli.HelpFlag.Name = "help, ?"

	// Bootstrap using action-registered commandline flags
	app := cli.NewApp()
	app.Name = "speedboat"
	app.Usage = "A next-generation load generator"
	app.Version = "0.0.1a1"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "verbose, v",
			Usage: "More verbose output",
		},
		cli.StringFlag{
			Name:  "script, s",
			Usage: "Script to run (do not use with --url)",
		},
		cli.StringFlag{
			Name:  "url",
			Usage: "URL to test (do not use with --script)",
		},
		cli.IntFlag{
			Name:  "vus, u",
			Usage: "Number of VUs to simulate",
			Value: 10,
		},
		cli.DurationFlag{
			Name:  "duration, d",
			Usage: "Test duration",
			Value: time.Duration(10) * time.Second,
		},
		cli.StringFlag{
			Name:  "out-file, o",
			Usage: "Output raw metrics to a file",
		},
	}
	app.Before = func(c *cli.Context) error {
		configureLogging(c)
		return nil
	}
	app.Action = action
	app.Run(os.Args)
}
