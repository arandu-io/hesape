// Package grammars mirrors Illuminate\Database\Schema\Grammars.
//
// The source of truth is the clone at
// laravel_illuminate/database/Schema/Grammars, commit
// d59e7abde7bc62bb2a52b4fb0651327acf8d0d0a ("Merge branch '13.x'", 2026-03-02).
//
// The files it answers to:
//
//	Grammar.php        -> BaseGrammar
//	MySqlGrammar.php   -> MySQLGrammar
//	PostgresGrammar.php-> PostgresGrammar
//	SQLiteGrammar.php  -> SQLiteGrammar
//
// A grammar turns a schema.Blueprint into statements. It executes nothing and
// takes no auth.Grant: schema.Builder is the half that runs what is compiled
// here, and that is where the Grant is checked.
//
// # What the three do not agree on
//
// The column type is where they diverge most, and the divergence is not
// cosmetic:
//
//	string    varchar(255)   varchar(255)              varchar
//	text      text           text                      text
//	json      json           json                      text (or json)
//	jsonb     json           jsonb                     text (or jsonb)
//	boolean   tinyint(1)     boolean                   tinyint(1)
//	binary    blob           bytea                     blob
//	uuid      char(36)       uuid                      varchar
//	increment auto_increment serial / bigserial        primary key autoincrement
//
// SQLite has five storage classes and no varchar length, which is why a
// migration that works there and nowhere else is the failure the conformance
// suite exists to catch. Its json and jsonb depend on the use_native_json and
// use_native_jsonb connection options, as they do in PHP.
//
// # MariaDbGrammar and SqlServerGrammar
//
// MariaDbGrammar overrides two type methods on MySqlGrammar and nothing else;
// both overrides are reachable here from Connection.IsMaria, so MySQLGrammar
// serves MariaDB and there is no second type. SqlServerGrammar is a driver this
// ecosystem does not carry.
//
// # How a driver grammar extends the base
//
// PHP's abstract Grammar calls back into the subclass through late static
// binding: getColumn calls $this->getType(), and the subclass's typeString
// answers. Go embedding has no such dispatch, so each driver grammar hands
// itself to BaseGrammar at construction and the base calls back through an
// unexported dialect interface -- typeOf, modify and wrapValue. Everything the
// PHP abstract class implements by throwing is implemented in BaseGrammar by
// returning an error wrapping schema.ErrUnsupported, so a driver that does not
// override it reports the same refusal.
package grammars
