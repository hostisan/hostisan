package httpapi

import (
	"context"
	"html"
	"os"
	"strings"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/mailer"
)

// renderEmail builds an account email (Plan 3: welcome, password reset, 2FA unlock)
// the way Hestia's PHP does. The caller sets To and sends it with s.Mailer; a send
// failure is logged without any secret and never changes the reply.
//
//   - lang is the recipient's language ("" means the system LANGUAGE, then "en").
//     The template <lang>/<template>.html is tried first, then <template>.html
//     (EmailTemplates.Load). Without one, the body is fallbackBody (an English msgid)
//     translated from lang's catalog.
//   - The subject is the template's <subject>, or else defaultSubject (translated when
//     the catalog has it as is) wrapped in SUBJECT_EMAIL ("{{subject}}" when unset).
//   - Placeholders are written {{name}}; the maps are keyed by name without braces.
//     In the subject, {{hostname}} (the system hostname), {{appname}} and subjectVars
//     are filled as is. In the body, {{hostname}} is the system hostname unless
//     bodyVars sets "hostname"; every bodyVars value (and the hostname) is
//     HTML-escaped; only {{appname}} (APP_NAME through brand.Rewrite) is not.
func (s *server) renderEmail(ctx context.Context, cfg hestia.SysConfig, lang, template, fallbackBody, defaultSubject string,
	subjectVars, bodyVars map[string]string) mailer.Message {
	lang = firstNonEmpty(lang, cfg["LANGUAGE"], "en")
	var subject, body string
	var found bool
	if s.EmailTemplates != nil {
		subject, body, found = s.EmailTemplates.Load(lang, template)
	}
	if !found {
		body = s.acctTranslate(lang, fallbackBody)
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		wrap := cfg["SUBJECT_EMAIL"]
		if wrap == "" {
			wrap = "{{subject}}"
		}
		subject = strings.ReplaceAll(wrap, "{{subject}}", s.acctTranslate(lang, defaultSubject))
	}
	app := brand.Rewrite(cfg["APP_NAME"])

	pairs := []string{"{{hostname}}", s.acctHostname(), "{{appname}}", app}
	for k, v := range subjectVars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	subject = strings.NewReplacer(pairs...).Replace(subject)

	pairs = []string{"{{appname}}", app}
	if _, ok := bodyVars["hostname"]; !ok {
		pairs = append(pairs, "{{hostname}}", html.EscapeString(s.acctHostname()))
	}
	for k, v := range bodyVars {
		if k != "appname" {
			pairs = append(pairs, "{{"+k+"}}", html.EscapeString(v))
		}
	}
	body = strings.NewReplacer(pairs...).Replace(body)
	return mailer.Message{Subject: subject, Body: body}
}

// acctTranslate returns msgid in lang from the gettext catalogs, or msgid itself when
// the language or the entry is missing.
func (s *server) acctTranslate(lang, msgid string) string {
	if s.catalogs == nil || msgid == "" {
		return msgid
	}
	if msgs, err := s.catalogs.Messages(lang); err == nil && msgs[msgid] != "" {
		return msgs[msgid]
	}
	return msgid
}

// acctHostname is the system hostname used in emails and links.
func (s *server) acctHostname() string {
	if s.Sessions != nil && s.Sessions.SystemHostname != "" {
		return s.Sessions.SystemHostname
	}
	h, _ := os.Hostname()
	return h
}
