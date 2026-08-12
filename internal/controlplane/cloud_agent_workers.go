package controlplane

import (
	"context"
	"encoding/json"
	"time"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/newsletter"
)

func (s *Server) cloudOrchestrator() *appservice.CloudOrchestrator {
	location, err := time.LoadLocation(s.CloudAgentTimezone)
	if err != nil {
		location = time.UTC
	}
	service := s.agentApp()
	return &appservice.CloudOrchestrator{
		Service: service, Knowledge: &appservice.ServiceKnowledgeSink{Service: service},
		COSAgentID: s.COSAgentID, NewsletterAgentID: s.NewsletterAgentID, Location: location,
	}
}

func (s *Server) cloudAgentLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	worker := s.cloudOrchestrator()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if worked, err := worker.ProcessOutlookItem(ctx, s.workerID); err != nil {
				s.Logger.Printf("outlook knowledge worker: %v", err)
			} else if worked {
				continue
			}
			if s.COSAgentID != "" {
				if _, err := worker.ProcessCOSBriefing(ctx, s.workerID); err != nil {
					s.Logger.Printf("COS orchestration worker: %v", err)
				}
			}
			if s.NewsletterAgentID != "" {
				if _, err := worker.ProcessNewsletterSynthesis(ctx, s.workerID); err != nil {
					s.Logger.Printf("newsletter orchestration worker: %v", err)
				}
			}
		}
	}
}

func (s *Server) newsletterScheduleLoop(ctx context.Context) {
	if s.NewsletterAgentID == "" {
		return
	}
	run := func() {
		if err := s.scheduleDailyNewsletter(ctx, time.Now()); err != nil {
			s.Logger.Printf("newsletter scheduler: %v", err)
		}
	}
	run()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (s *Server) scheduleDailyNewsletter(ctx context.Context, now time.Time) error {
	location, err := time.LoadLocation(s.CloudAgentTimezone)
	if err != nil {
		location = time.UTC
	}
	day := now.In(location).Format("2006-01-02")
	if _, err = s.DB.GetCloudOrchestrationTask(ctx, "newsletter_synthesis", day); err == nil {
		return nil
	}
	credentials := newsletter.EnvironmentCredentials{}
	client := newsletter.NewPublicHTTPClient()
	service := s.agentApp()
	orchestrator := &appservice.NewsletterOrchestrator{Service: service, Collectors: appservice.CollectorRegistry{
		"rss": &newsletter.FeedCollector{Client: client}, "atom": &newsletter.FeedCollector{Client: client},
		"web":    &newsletter.WebCollector{Client: client},
		"reddit": &newsletter.RedditCollector{Client: client, Credentials: credentials},
		"x":      &newsletter.XCollector{Client: client, Credentials: credentials},
	}}
	report, err := orchestrator.Collect(ctx)
	if err != nil {
		return err
	}
	if report.Succeeded == 0 {
		return nil
	}
	currentItems, err := s.DB.ListNewsletterItemsSince(ctx, day+"T00:00:00Z")
	if err != nil {
		return err
	}
	if len(currentItems) == 0 {
		return nil
	}
	payload := map[string]any{"date": day, "sources_succeeded": report.Succeeded, "items": report.Items, "source_failures": report.Failures}
	_, err = s.DB.EnqueueCloudOrchestrationTask(ctx, "newsletter_synthesis", day, cloudPayloadJSON(payload))
	return err
}

func cloudPayloadJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
