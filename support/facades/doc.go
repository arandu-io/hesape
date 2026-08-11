// Package facades mirrors Illuminate\Support\Facades.
//
// Nothing is built here, and nothing will be: ADR 0002 rejected facades. A
// facade is __callStatic over a container lookup, and this framework has
// neither. What a facade reaches, the package that owns it exports directly.
//
// The list is kept because it is the exact index of what a Laravel application
// touches. The files, in the clone at laravel_illuminate/support/Facades:
//
//	App.php             Artisan.php         Auth.php            Blade.php
//	Broadcast.php       Bus.php             Cache.php           Concurrency.php
//	Config.php          Context.php         Cookie.php          Crypt.php
//	DB.php              Date.php            Event.php           Exceptions.php
//	Facade.php          File.php            Gate.php            Hash.php
//	Http.php            Lang.php            Log.php             Mail.php
//	Notification.php    ParallelTesting.php Password.php        Pipeline.php
//	Process.php         Queue.php           RateLimiter.php     Redirect.php
//	Redis.php           Request.php         Response.php        Route.php
//	Schedule.php        Schema.php          Session.php         Storage.php
//	URL.php             Validator.php       View.php            Vite.php
//
// Date is the one whose seam still exists, because a test has to be able to
// move the clock: it is support.Now, support.Travel and support.FreezeTime.
package facades
