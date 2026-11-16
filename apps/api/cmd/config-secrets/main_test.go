package main

import (
	"reflect"
	"testing"
)

func ctr(service string, env ...string) container {
	var c container
	c.Config.Labels = map[string]string{"com.docker.compose.service": service}
	c.Config.Env = env
	return c
}

func TestPlanReproducesTheDeployment(t *testing.T) {
	containers := []container{
		ctr("oauth", "JWT_SECRET=j", "KUN_TRUST_CLIENT_SECRET=o", "KUN_IMAGE_S3_BUCKET=b", "KUN_PG_PASSWORD=p"),
		ctr("catalog", "JWT_SECRET=j", "KUN_TRUST_CLIENT_SECRET=f", "KUN_IMAGE_S3_BUCKET=b"),
		ctr("chat", "JWT_SECRET=j", "KUN_TRUST_CLIENT_SECRET=", "KUN_IMAGE_S3_BUCKET=b", "KUN_CHAT_CENTRIFUGO_API_KEY=c"),
		ctr("postgres", "POSTGRES_PASSWORD=p"),
	}
	got := plan(containers, true, wanted("KUN_IMAGE_S3_BUCKET"))
	want := []row{
		{"", "JWT_SECRET", "j"},
		{"", "KUN_IMAGE_S3_BUCKET", "b"},
		{"catalog", "KUN_TRUST_CLIENT_SECRET", "f"},
		{"oauth", "KUN_TRUST_CLIENT_SECRET", "o"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan =\n%v\nwant\n%v", got, want)
	}
}

func TestPlanWithoutGlobalKeepsEveryRowScoped(t *testing.T) {
	got := plan([]container{ctr("agent", "JWT_SECRET=j")}, false, wanted(""))
	if !reflect.DeepEqual(got, []row{{"agent", "JWT_SECRET", "j"}}) {
		t.Fatalf("plan = %v", got)
	}
}
