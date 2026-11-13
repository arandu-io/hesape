package validation_test

import (
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/translation"
	"github.com/arandu-io/hesape/validation"
)

func commonPolicy() *validation.Password {
	return validation.PasswordMin(12).Max(128).Uncompromised(validation.CommonPasswords())
}

// refusedAsCommon asks the verifier alone, so that a short list entry is judged
// by the lists and not by the length rule of the policy around it.
func refusedAsCommon(password string) bool {
	return !validation.CommonPasswords().Verify(password, 0)
}

func TestCommonPasswordsRefuses(t *testing.T) {
	cases := map[string][]string{
		"an embedded list entry, in any case": {
			"password", "PASSWORD", "Password", "iloveyou", "football", "monkey", "dragon", "letmein",
			"trustno1", "baseball", "superman", "sunshine", "master", "welcome", "shadow", "michael",
		},
		"a Brazilian entry": {
			"senha", "senha123", "brasil", "flamengo", "corinthians", "amorzinho", "mudar123",
			"qwertyuiop", "palmeiras", "minhasenha", "teamo", "deusefiel",
		},
		"a trailing run of digits and symbols": {
			"password1234", "Password12345678", "Senha@2026!", "qwerty123456", "Flamengo2026!!",
			"brasil20262026", "letmein!!!!!!!", "football#2026$", "monkey1234567", "Corinthians@1910",
			"iloveyou2026!!", "mudar123456789", "Amorzinho<3<3<3", "senha@123456789",
		},
		"a leading run": {
			"2026password!!", "123456senhaforte", "!!!!!!flamengo", "123123password",
		},
		"leetspeak": {
			"p@ssw0rd", "P@ssw0rd2026!", "p@$$w0rd1234", "pa55word1234", "s3nh@2026!!!!", "br@s1l2026!!!",
			"f1@m3ng0!!!!!", "1l0v3y0u2026!", "l3tm31n!!!!!", "dr@g0n123456!", "m0nk3y!!!!!!!", "Fl@mengo123456",
		},
		"accents": {
			"SENHÁ2026!!!!", "Brasíl123456!",
		},
		"the list word repeated": {
			"passwordpassword", "PasswordPassword", "senhasenhasenha", "monkeymonkey", "dragondragon",
			"letmeinletmein", "football1football1", "iloveyouiloveyou", "123456123456", "1234123412341234",
		},
		"one repeated character": {
			"aaaaaaaaaaaa", "111111111111", "!!!!!!!!!!!!", "zzzzzzzzzzzzzzzzzz", "000000000000", "QQQQQQQQQQQQ",
		},
		"a short unit repeated": {
			"abababababab", "123123123123", "xyxyxyxyxyxy", "a1a1a1a1a1a1", "abcabcabcabc",
		},
		"an ascending or descending run": {
			"123456789012", "1234567890123456", "abcdefghijkl", "ABCDEFGHIJKL", "987654321098",
			"zyxwvutsrqpo", "0123456789ab"[:10] + "01", "456789012345", "mnopqrstuvwx", "xyzabcdefghi",
		},
		"a keyboard walk": {
			"qwertyuiopas", "1q2w3e4r5t6y", "asdfghjklzxc", "qazwsxedcrfv", "1qaz2wsx3edc", "zxcvbnmasdfg",
			"lkjhgfdsapoi", "q1w2e3r4t5y6", "a1s2d3f4g5h6", "qwertyuiop12", "1234567890qw", "qwertyuiopasdfghjkl",
			"QWERTYUIOPAS", "mnbvcxzlkjhg", "!@#$%^&*()",
		},
		"patterns chained": {
			"qwertyuiop1234567890", "abcdefgh12345678", "aaaaaaaa12345678", "123456789qwertyuiop",
		},
	}
	for kind, passwords := range cases {
		for _, password := range passwords {
			t.Run(kind+"/"+password, func(t *testing.T) {
				if !refusedAsCommon(password) {
					t.Errorf("%q must be refused", password)
				}
			})
		}
	}
}

func TestCommonPasswordsAllowsAStrongPassword(t *testing.T) {
	passwords := []string{
		// Passphrases of unrelated words, with and without separators.
		"correct horse battery staple", "correcthorsebatterystaple", "Correct-Horse-Battery-Staple-7",
		"purple giraffe dances quietly", "my cat eats blue cheese on tuesdays", "o gato comeu o queijo azul ontem",
		"cafe com leite e pao na chapa", "Uma-Casa-Branca-No-Alto-Do-Morro", "vento frio sobre a serra verde",
		"the quick brown fox jumps over", "tres tristes tigres no trigal!", "sol&lua&mar&terra&fogo&ar",
		// Random passwords.
		"k9#Qm2$vLp7@Zx4n", "Xt7!pR2^mQ9&wL4z", "gH3$kP8#nV5!bY1%", "vB6&zN2*mK9@qT4#", "7f!Kd3$Lw9@Rq2^c",
		"R4v#8Tn$2Lm@9Qp!", "mZ2@xC7!bN4$vK9#", "Q8w$3Er#5Ty@7Ui!", "u7Y!t5R@e3W#q1A$", "pL0&oK9*iJ8^uH7%",
		"a8Zk2Qm9Xv4Lc7Wb", "T9bR4nX2vQ7mK5zH", "4hGf9Dk2Jp7Ls3Qa", "zX9cV2bN7mQ4wE6r", "L3m9Qz2Xk7Vc4Bn8",
		"h7N#d2W!k9S$m4Q%", "E5r!T8y@U2i#O6p$", "9aS$2dF#4gH@6jK!", "w3E#r5T$y7U%i9O^", "bN8*mV5&cX2^zL9@",
		// Words from the lists with something of one's own around them.
		"Tr0ub4dor&3-horse-staple", "baseball-Zq81-purple-tree", "my password is a long sentence!", "dragon-slayer-of-the-north-7",
		"senha do wifi da minha avo e longa", "flamengo-campeao-em-casa-de-novo!", "Brasil-Pais-Do-Futuro-Verde-Amarelo",
		// Unicode.
		"日本語のパスワードは長い方がいい", "пароль-очень-длинный-и-странный", "ëÿñüçãõ-abc-xyz-987-ÀÉÎÕÜ", "😀🔒🔑-correct-horse-9",
		"Þórr-Óðinn-Freyja-Baldur-Loki", "γειά σου κόσμε και καλημέρα",
	}
	for _, password := range passwords {
		t.Run(password, func(t *testing.T) {
			if refusedAsCommon(password) {
				t.Errorf("%q must pass: it is not a common password", password)
			}
		})
	}
}

// TestCommonPasswordsDoesNotRefuseWhatIsRandom is the other side of the
// normalisation: folding and trimming widen what is refused, and this is what
// keeps them from reaching a password nobody could have guessed. The seed is
// fixed, so a failure names a password that can be run again.
func TestCommonPasswordsDoesNotRefuseWhatIsRandom(t *testing.T) {
	v := validation.CommonPasswords()
	rng := rand.New(rand.NewPCG(20261113, 105))

	alphabets := []string{
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+",
		"abcdefghijklmnopqrstuvwxyz",
		"abcdefghijklmnopqrstuvwxyz0123456789",
	}
	for i := 0; i < 40000; i++ {
		alphabet := alphabets[i%len(alphabets)]
		size := 12 + rng.IntN(13)
		b := make([]byte, size)
		for j := range b {
			b[j] = alphabet[rng.IntN(len(alphabet))]
		}
		if !v.Verify(string(b), 0) {
			t.Fatalf("the random password %q was refused", b)
		}
	}

	words := strings.Fields(`apple river mountain window carpet lantern violin pepper harbor meadow
		thunder orchid saddle velvet anchor bridge candle desert engine forest garden island jungle kettle
		ladder marble needle ocean planet quartz rocket silver timber umbrella valley whisper yellow zipper
		cabana dente janela estrada pedra fogueira cachoeira sertao vereda biscoito telhado montanha palmeira
		girassol travessia horizonte lavanda mangue oficina pomar quintal riacho sombra trilha varanda`)
	for i := 0; i < 20000; i++ {
		separator := []string{" ", "-", "", ".", "_"}[i%5]
		picked := make([]string, 0, 4)
		seen := map[string]bool{}
		for len(picked) < 4 {
			w := words[rng.IntN(len(words))]
			if !seen[w] {
				seen[w] = true
				picked = append(picked, w)
			}
		}
		phrase := strings.Join(picked, separator)
		if !v.Verify(phrase, 0) {
			t.Fatalf("the passphrase %q was refused", phrase)
		}
	}
}

func TestCommonPasswordsVerifyIgnoresTheThreshold(t *testing.T) {
	v := validation.CommonPasswords()
	for _, threshold := range []int{0, 1, 100, -1} {
		if v.Verify("password123456", threshold) {
			t.Errorf("threshold %d changed the answer for a common password", threshold)
		}
		if !v.Verify("k9#Qm2$vLp7@Zx4n", threshold) {
			t.Errorf("threshold %d changed the answer for a strong password", threshold)
		}
	}
}

func TestCommonPasswordsShortInputsAreNotCutToNothing(t *testing.T) {
	v := validation.CommonPasswords()
	// A word shorter than the length of a useful core is only refused when it
	// is the whole text, never as the first letters of a long password.
	for _, password := range []string{"dog-Kx9#mQ2$vLp7", "ab-Kx9#mQ2$vLp7", "a1-Kx9#mQ2$vLp7"} {
		if !v.Verify(password, 0) {
			t.Errorf("%q starts with a short word and must pass", password)
		}
	}
	if !v.Verify("", 0) {
		t.Error("an empty value is not a common password: required is the rule that refuses it")
	}
}

func TestCommonPasswordsNeverPanicsOnOddInput(t *testing.T) {
	v := validation.CommonPasswords()
	inputs := []string{
		"", " ", "\x00", "\xff\xfe\xfd", "\xc3", "\xe2\x82", strings.Repeat("\xff", 300), "é", "ß", "İİİİİİİİİİİİ",
		strings.Repeat("é", 200), strings.Repeat("ç1", 70), strings.Repeat("日", 129), "á́́́́",
		strings.Repeat("password", 40), strings.Repeat("a", 100000), strings.Repeat("abcdefghijklmnopqrstuvwxyz", 10),
		strings.Repeat("0123456789", 20), strings.Repeat("😀", 500), "​​​​​​",
	}
	for _, in := range inputs {
		_ = v.Verify(in, 0)
	}
}

func TestCommonPasswordsLongInputs(t *testing.T) {
	v := validation.CommonPasswords()
	if v.Verify(strings.Repeat("a", 1000), 0) {
		t.Error("one character repeated a thousand times must be refused")
	}
	if v.Verify(strings.Repeat("0123456789", 30), 0) {
		t.Error("a sequential run of three hundred digits must be refused")
	}
	long := strings.Repeat("a", 200) + "k9#Qm2$vLp7@Zx4n"
	if !v.Verify(long, 0) {
		t.Error("a long input that is not entirely one pattern must pass")
	}
}

func TestCommonPasswordsAllocatesNothing(t *testing.T) {
	v := validation.CommonPasswords()
	v.Verify("warm-up", 0) // builds the tables
	for _, password := range []string{"k9#Qm2$vLp7@Zx4n", "Password1234!", "qwertyuiopas", "correct horse battery staple"} {
		allocs := testing.AllocsPerRun(50, func() { v.Verify(password, 0) })
		if allocs != 0 {
			t.Errorf("Verify(%q) allocated %v times", password, allocs)
		}
	}
}

func TestCommonPasswordsIsSafeForConcurrentUse(t *testing.T) {
	v := validation.CommonPasswords()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if v.Verify("Password1234!", 0) {
					t.Error("a common password passed")
					return
				}
				if !v.Verify("k9#Qm2$vLp7@Zx4n", 0) {
					t.Error("a strong password was refused")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestCommonPasswordsPolicyStillReportsUncompromised(t *testing.T) {
	applied := commonPolicy().AppliedRules()
	if applied["uncompromised"] != true {
		t.Fatalf("a form draws the rule from AppliedRules: got %v", applied)
	}
	if applied["min"] != 12 || applied["max"] != 128 {
		t.Fatalf("the other rules must be reported as they were: got %v", applied)
	}
}

func TestCommonPasswordsSaysTooCommonInEnglish(t *testing.T) {
	rule := commonPolicy()
	if rule.Passes("password", "Password1234!") {
		t.Fatal("a common password passed")
	}
	got := strings.Join(rule.Message(), " ")
	if !strings.Contains(got, "too common") || strings.Contains(got, "data leak") {
		t.Errorf("the sentence must say the password is too common, not that it leaked: %q", got)
	}
}

func TestCommonPasswordsSaysTooCommonInPortuguese(t *testing.T) {
	factory := validation.NewFactory(validation.FromTranslation(translation.New(nil, "pt-BR", "en")))
	set, err := factory.Compile(validation.Rules{"password": "required"})
	if err != nil {
		t.Fatal(err)
	}
	v := factory.Make(validation.Data{"password": "Senha@2026!!!"}, set)
	v.After(func(v *validation.Validator) {
		v.ValidateUsingCustomRule("password", v.GetValue("password"), commonPolicy())
	})
	got := v.Errors().Get("password")
	if len(got) != 1 || got[0] != "O valor informado para o campo password é muito comum. Escolha outro valor." {
		t.Errorf("got %q", got)
	}
}

// aLeakVerifier is a verifier that does not say why it refuses.
type aLeakVerifier struct{}

func (aLeakVerifier) Verify(string, int) bool { return false }

func TestAVerifierWithNoReasonStillSaysItLeaked(t *testing.T) {
	rule := validation.PasswordMin(1).Uncompromised(aLeakVerifier{})
	if rule.Passes("password", "abcdefgh") {
		t.Fatal("the verifier refuses everything")
	}
	if got := strings.Join(rule.Message(), " "); !strings.Contains(got, "data leak") {
		t.Errorf("got %q", got)
	}
}

func TestCommonPasswordsNilVerifierStillFailsClosed(t *testing.T) {
	if validation.PasswordMin(1).Uncompromised(nil).Passes("password", "k9#Qm2$vLp7@Zx4n") {
		t.Fatal("a policy with no verifier must refuse: nothing could be asked")
	}
}

func BenchmarkCommonPasswords(b *testing.B) {
	v := validation.CommonPasswords()
	v.Verify("warm-up", 0)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v.Verify("k9#Qm2$vLp7@Zx4n", 0)
		v.Verify("Password1234!", 0)
	}
}

// TestTheEmbeddedListIsCredited: the list is MIT licensed work that every binary
// linking this package carries, and the notice has to travel with it.
func TestTheEmbeddedListIsCredited(t *testing.T) {
	body, err := os.ReadFile("THIRD_PARTY.md")
	if err != nil {
		t.Fatalf("THIRD_PARTY.md is the notice the embedded list requires: %v", err)
	}
	notice := string(body)
	for _, text := range []string{
		"common_passwords.txt", "Daniel Miessler", "Permission is hereby granted, free of charge",
		"The above copyright notice and this permission notice shall be included",
	} {
		if !strings.Contains(notice, text) {
			t.Errorf("THIRD_PARTY.md is missing %q", text)
		}
	}
}
