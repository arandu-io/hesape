package mail

import (
	"crypto/rand"
	"encoding/hex"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// AttachedFile is one attachment named rather than made of bytes: File is a
// path, an [*Attachment] or an [Attachable], and Options is what it goes under.
type AttachedFile struct {
	File    any
	Options AttachOptions
}

// AttachedData is one attachment made of bytes, under a name.
type AttachedData struct {
	Data    []byte
	Name    string
	Options AttachOptions
}

// AttachedDisk is one attachment named by disk and path, read at send time.
type AttachedDisk struct {
	Disk    string
	Path    string
	Name    string
	Options AttachOptions
}

// embedded is a part referenced from the body by content id.
type embedded struct {
	CID  string
	Name string
	Mime string
	Data []byte
	Path string
}

// Message is the message itself, and what a Transport receives. There is no
// separate MIME object underneath it: the fields are what a transport reads.
//
// # Why From, To, CC, BCC, ReplyTo and Subject are not methods here
//
// They are fields, promoted from the embedded [Envelope], and a Go type cannot
// carry a method and a field of the same name. The setters of those names live
// on [PendingMail], which is the value that is doing the sending. Every Message
// method that has no field to collide with -- Sender, ReturnPath, ForgetTo,
// ForgetCC, ForgetBCC, Priority, Attach, AttachData, Embed, EmbedData -- is
// here.
type Message struct {
	Envelope

	// HTML and Text are the rendered parts. A transport reads them; the Mailer
	// is what fills them in, exactly once, so that two transports cannot render
	// the same mailable differently.
	HTML string
	Text string

	// Attachments, RawAttachments and DiskAttachments are the three kinds of
	// attachment, kept apart because only the first two can be compared
	// without touching a disk -- which is what makes HasAttachmentFromStorage
	// a query rather than a read.
	Attachments     []AttachedFile
	RawAttachments  []AttachedData
	DiskAttachments []AttachedDisk

	// Headers is what the mailable's Headers method asked for.
	Headers Headers

	// Locale is the locale the message was rendered in. The translator that
	// acts on it is wired by the application; nothing in this package switches
	// one.
	Locale string

	sender     Address
	returnPath string
	priority   int
	embeds     []embedded
	mailerName string

	// err is what a fluent setter could not report. Attach fails when an
	// attachment has no name to go under, and a method that returns the
	// receiver has nowhere to say so, so the failure is kept here and answered
	// by the first call that does have somewhere -- Build, or Send.
	err error
}

// Err is what went wrong in a fluent call that had no way to say so.
func (m *Message) Err() error { return m.err }

// sameFile compares two attachment sources for the dedupe. It is a function
// rather than == because AttachedFile.File is an any, and comparing two
// interface values whose dynamic type is not comparable is a runtime panic --
// one that would fire on a slice somebody passed by mistake, in the middle of
// sending.
func sameFile(a, b any) bool {
	left, leftOK := a.(string)
	right, rightOK := b.(string)
	if leftOK && rightOK {
		return left == right
	}
	if leftOK != rightOK {
		return false
	}
	leftAttachment, leftOK := a.(*Attachment)
	rightAttachment, rightOK := b.(*Attachment)
	if leftOK && rightOK {
		return leftAttachment == rightAttachment
	}
	return false
}

// GetSymfonyMessage is the underlying MIME message, which is this value: there
// is no separate object to unwrap, so the method returns the receiver.
func (m *Message) GetSymfonyMessage() *Message { return m }

// Sender sets the Sender header, which is who actually submitted the message
// when that is not who it is from.
func (m *Message) Sender(address any, name ...string) *Message {
	if list := addressesOf(address, name...); len(list) > 0 {
		m.sender = list[0]
	}
	return m
}

// GetSender is the Sender header, for a transport that has to write it.
func (m *Message) GetSender() Address { return m.sender }

// ReturnPath sets the address bounces go to.
func (m *Message) ReturnPath(address string) *Message {
	m.returnPath = address
	return m
}

// GetReturnPath is the bounce address, for a transport that has to write it.
func (m *Message) GetReturnPath() string { return m.returnPath }

// Priority sets the message priority, where 1 is the highest and 5 the lowest.
func (m *Message) Priority(level int) *Message {
	m.priority = level
	return m
}

// GetPriority is the priority, zero when none was set.
func (m *Message) GetPriority() int { return m.priority }

// ForgetTo removes every "to" address, recording what it removed in an X-To
// header.
//
// The debug header is the point of the method: the global "to" of a
// development mailer redirects everything to one inbox, and without X-To the
// message that arrives no longer says who it was for.
func (m *Message) ForgetTo() *Message {
	if len(m.To) > 0 {
		m.addAddressDebugHeader("X-To", m.To)
		m.To = nil
	}
	return m
}

// ForgetCC removes every carbon copy address, recording what it removed in an
// X-Cc header.
func (m *Message) ForgetCC() *Message {
	if len(m.CC) > 0 {
		m.addAddressDebugHeader("X-Cc", m.CC)
		m.CC = nil
	}
	return m
}

// ForgetBCC removes every blind carbon copy address, recording what it removed
// in an X-Bcc header.
func (m *Message) ForgetBCC() *Message {
	if len(m.BCC) > 0 {
		m.addAddressDebugHeader("X-Bcc", m.BCC)
		m.BCC = nil
	}
	return m
}

func (m *Message) addAddressDebugHeader(header string, list []Address) {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.String())
	}
	if m.Headers.Text == nil {
		m.Headers.Text = map[string]string{}
	}
	m.Headers.Text[header] = strings.Join(out, ", ")
}

// Attach adds a file to the message. The file is a path, an [*Attachment] or an
// [Attachable].
func (m *Message) Attach(file any, options ...AttachOptions) *Message {
	o := firstOption(options)

	if a, ok := file.(Attachable); ok {
		file = a.ToMailAttachment()
	}
	if a, ok := file.(*Attachment); ok {
		if err := a.AttachTo(m, o); err != nil && m.err == nil {
			m.err = err
		}
		return m
	}

	// Deduped on the file, so attaching the same path twice sends one part
	// rather than two.
	for _, existing := range m.Attachments {
		if sameFile(existing.File, file) {
			return m
		}
	}
	m.Attachments = append(m.Attachments, AttachedFile{File: file, Options: o})
	return m
}

// AttachMany adds several files at once. An element is a path, an attachment,
// or an [AttachedFile] when a file needs options of its own.
func (m *Message) AttachMany(files ...any) *Message {
	for _, file := range files {
		if with, ok := file.(AttachedFile); ok {
			m.Attach(with.File, with.Options)
			continue
		}
		m.Attach(file)
	}
	return m
}

// AttachData adds bytes as an attachment under the given name.
func (m *Message) AttachData(data []byte, name string, options ...AttachOptions) *Message {
	o := firstOption(options)
	for _, existing := range m.RawAttachments {
		if existing.Name == name && string(existing.Data) == string(data) {
			return m
		}
	}
	m.RawAttachments = append(m.RawAttachments, AttachedData{Data: data, Name: name, Options: o})
	return m
}

// AttachFromStorage attaches a path on the default disk. An empty name means
// the path's last segment.
func (m *Message) AttachFromStorage(p, name string, options ...AttachOptions) *Message {
	return m.AttachFromStorageDisk("", p, name, options...)
}

// AttachFromStorageDisk attaches a path on the named disk.
//
// Nothing is read here: the row is recorded and the disk is asked at send time,
// which is what lets a queued mailable carry an attachment across a process.
func (m *Message) AttachFromStorageDisk(disk, p, name string, options ...AttachOptions) *Message {
	if name == "" {
		name = path.Base(p)
	}
	o := firstOption(options)
	for _, existing := range m.DiskAttachments {
		if existing.Name+existing.Disk+existing.Path == name+disk+p {
			return m
		}
	}
	m.DiskAttachments = append(m.DiskAttachments, AttachedDisk{Disk: disk, Path: p, Name: name, Options: o})
	return m
}

func (m *Message) attachPath(p string, o AttachOptions) {
	m.Attachments = append(m.Attachments, AttachedFile{File: p, Options: o})
}

func (m *Message) attachBytes(data []byte, name string, o AttachOptions) {
	m.RawAttachments = append(m.RawAttachments, AttachedData{Data: data, Name: name, Options: o})
}

// Embed puts a file in the message and answers the cid: reference the body uses
// to show it inline.
func (m *Message) Embed(file any) string {
	if a, ok := file.(Attachable); ok {
		file = a.ToMailAttachment()
	}
	if a, ok := file.(*Attachment); ok {
		cid := ""
		a.AttachWith(
			func(p string) any {
				cid = m.addEmbed(embedded{Path: p, Name: firstNonEmpty(a.as, filepath.Base(p)), Mime: a.mime})
				return nil
			},
			func(data func() ([]byte, error)) any {
				b, err := data()
				if err != nil {
					return nil
				}
				cid = m.addEmbed(embedded{Data: b, Name: a.as, Mime: a.mime})
				return nil
			},
		)
		return cid
	}

	p, _ := file.(string)
	return m.addEmbed(embedded{Path: p, Name: filepath.Base(p)})
}

// EmbedData puts bytes in the message and answers the cid: reference for them.
// The content type is optional and arrives as a variadic tail.
func (m *Message) EmbedData(data []byte, name string, contentType ...string) string {
	part := embedded{Data: data, Name: name}
	if len(contentType) > 0 {
		part.Mime = contentType[0]
	}
	return m.addEmbed(part)
}

func (m *Message) addEmbed(part embedded) string {
	if part.Mime == "" {
		part.Mime = mimeTypeOf(part.Name)
	}
	part.CID = newContentID()
	m.embeds = append(m.embeds, part)
	return "cid:" + part.CID
}

func newContentID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A content id only has to be unique inside one message; a counter is
		// not available here and a failed read is not a reason to lose the part.
		return "part@arandu"
	}
	return hex.EncodeToString(b[:]) + "@arandu"
}

func mimeTypeOf(name string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		return t
	}
	return "application/octet-stream"
}

// UsesMailer reports whether this message goes out through the named mailer.
func (m *Message) UsesMailer(mailer string) bool { return m.mailerName == mailer }

// HasFrom reports whether the message is from the given address.
func (m *Message) HasFrom(address string, name ...string) bool { return m.IsFrom(address, name...) }

// HasAttachment reports whether the file is already attached. The file is a
// path, an [*Attachment] or an [Attachable], as it is for Attach.
func (m *Message) HasAttachment(file any, options ...AttachOptions) bool {
	o := firstOption(options)

	if a, ok := file.(Attachable); ok {
		file = a.ToMailAttachment()
	}
	if a, ok := file.(*Attachment); ok {
		found := false
		a.AttachWith(
			func(p string) any {
				file = p
				o = AttachOptions{As: firstNonEmpty(o.As, a.as), Mime: firstNonEmpty(o.Mime, a.mime)}
				return nil
			},
			func(data func() ([]byte, error)) any {
				b, err := data()
				if err != nil {
					return nil
				}
				found = m.HasAttachedData(b,
					firstNonEmpty(o.As, a.as),
					AttachOptions{Mime: firstNonEmpty(o.Mime, a.mime)})
				return nil
			},
		)
		if found {
			return true
		}
		if _, isPath := file.(string); !isPath {
			return false
		}
	}

	for _, existing := range m.Attachments {
		if sameFile(existing.File, file) && existing.Options == o {
			return true
		}
	}
	return false
}

// HasAttachedData reports whether these exact bytes are attached under this
// name.
func (m *Message) HasAttachedData(data []byte, name string, options ...AttachOptions) bool {
	o := firstOption(options)
	for _, existing := range m.RawAttachments {
		if string(existing.Data) == string(data) && existing.Name == name && existing.Options == o {
			return true
		}
	}
	return false
}

// HasAttachmentFromStorage reports whether a path on the default disk is
// attached. An empty name means the path's last segment.
func (m *Message) HasAttachmentFromStorage(p, name string, options ...AttachOptions) bool {
	return m.HasAttachmentFromStorageDisk("", p, name, options...)
}

// HasAttachmentFromStorageDisk reports whether a path on the named disk is
// attached.
func (m *Message) HasAttachmentFromStorageDisk(disk, p, name string, options ...AttachOptions) bool {
	if name == "" {
		name = path.Base(p)
	}
	o := firstOption(options)
	for _, existing := range m.DiskAttachments {
		if existing.Disk == disk && existing.Path == p && existing.Name == name && existing.Options == o {
			return true
		}
	}
	return false
}

// parts is every attachment resolved to bytes, which is what [Render] writes.
// A disk attachment is read here, and a path attachment off the local
// filesystem is read here; a read that fails drops the part rather than the
// message, because a missing logo is not a reason for an invoice not to arrive.
func (m *Message) parts() []embedded {
	out := make([]embedded, 0, len(m.Attachments)+len(m.RawAttachments)+len(m.DiskAttachments))

	for _, a := range m.Attachments {
		p, ok := a.File.(string)
		if !ok {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		name := firstNonEmpty(a.Options.As, filepath.Base(p))
		out = append(out, embedded{
			Name: name,
			Mime: firstNonEmpty(a.Options.Mime, mimeTypeOf(name)),
			Data: data,
		})
	}

	for _, a := range m.RawAttachments {
		out = append(out, embedded{
			Name: a.Name,
			Mime: firstNonEmpty(a.Options.Mime, mimeTypeOf(a.Name)),
			Data: a.Data,
		})
	}

	for _, a := range m.DiskAttachments {
		if Filesystem == nil {
			continue
		}
		disk := Filesystem.Disk(a.Disk)
		data, err := disk.Get(a.Path)
		if err != nil {
			continue
		}
		mimeType := a.Options.Mime
		if mimeType == "" {
			if guessed, err := disk.MimeType(a.Path); err == nil {
				mimeType = guessed
			}
		}
		out = append(out, embedded{
			Name: a.Name,
			Mime: firstNonEmpty(mimeType, mimeTypeOf(a.Name)),
			Data: data,
		})
	}

	return out
}
