package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestXmageMineFixture(t *testing.T) {
	files, err := scanTree("testdata/Mage.Sets/src/mage/cards")
	if err != nil {
		t.Fatal(err)
	}
	got := census(files, 200)
	want := []string{
		"total\tcards\t3",
		"total\tcards_shared_only\t2",
		"total\tcards_with_private_helper\t1",
		"total\tshared_classes\t4",
		"class\tmage.abilities.Ability\t2",
		"class\tmage.constants.Duration\t2",
		"class\tmage.MageInt\t1",
		"class\tmage.constants.*\t1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("census mismatch\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestScanFileIgnoresCommentsAndStrings(t *testing.T) {
	src := []byte(`package mage.cards.a;

import java.util.UUID;
import mage.cards.CardImpl;
import mage.constants.Duration;
import static mage.abilities.Ability.someStaticHelper;

// class LineComment {}
/*
enum BlockComment { ONE, TWO }
*/
public final class Quiet extends CardImpl {

    private static final String NAME = "class InString {}";
    private static final char QUOTE = '"';
    private static final String BLOCK = """
            class InTextBlock {}
            """;

    private static final class RealHelper {
    }

    private enum RealEnum { ONE }
}
`)

	f, err := scanFile("Quiet.java", src)
	if err != nil {
		t.Fatal(err)
	}
	if f.Class != "Quiet" {
		t.Errorf("Class = %q, want Quiet", f.Class)
	}
	wantImports := []string{"mage.constants.Duration"}
	if !reflect.DeepEqual(f.Imports, wantImports) {
		t.Errorf("Imports = %q, want %q", f.Imports, wantImports)
	}
	wantPrivate := []string{"RealHelper", "RealEnum"}
	if !reflect.DeepEqual(f.Private, wantPrivate) {
		t.Errorf("Private = %q, want %q", f.Private, wantPrivate)
	}
}

func TestCensusIsSorted(t *testing.T) {
	files := []cardFile{
		{Class: "Zeta", Imports: []string{"mage.z.Z"}},
		{Class: "Alpha", Imports: []string{"mage.z.Z", "mage.a.A"}},
		{Class: "Beta", Imports: []string{"mage.z.Z", "mage.a.A", "mage.m.M"}},
		{Class: "Epsilon", Imports: []string{"mage.b.B"}},
		{Class: "Delta", Imports: []string{"mage.c.C"}},
	}
	shuffled := []cardFile{files[3], files[0], files[4], files[2], files[1]}

	for _, top := range []int{200, 3} {
		want := census(files, top)
		got := census(shuffled, top)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("top %d: shuffled census differs\ngot:\n%s\nwant:\n%s", top, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}

	want := []string{
		"total\tcards\t5",
		"total\tcards_shared_only\t5",
		"total\tcards_with_private_helper\t0",
		"total\tshared_classes\t5",
		"class\tmage.z.Z\t3",
		"class\tmage.a.A\t2",
		"class\tmage.b.B\t1",
		"class\tmage.c.C\t1",
		"class\tmage.m.M\t1",
	}
	if got := census(shuffled, 200); !reflect.DeepEqual(got, want) {
		t.Fatalf("census mismatch\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
