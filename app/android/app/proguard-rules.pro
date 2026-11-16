# flutter_local_notifications serializes notification details with Gson and
# reads them back via TypeToken subclasses. R8 strips generic signatures by
# default, which makes `new TypeToken<...>(){}` throw at runtime
# ("TypeToken must be created with a type argument") — e.g. on cancel().
-keepattributes Signature
-keepattributes *Annotation*
-keep class com.google.gson.reflect.TypeToken { *; }
-keep class * extends com.google.gson.reflect.TypeToken
-keep class com.dexterous.** { *; }
