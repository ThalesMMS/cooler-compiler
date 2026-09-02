class Main inherits IO {
  main() : Object {{
    out_int("😀é".length()); out_string("/");
    out_string("😀é".substr(1, 1)); out_string("/");
    out_string("😀".concat("é"));
  }};
};
