class Main inherits IO {
  main() : Object {
    let object : Object <- (new Main).copy() in {
      out_string(object.type_name());
      out_string("/");
      out_int("abc".length());
      out_string("/");
      out_string("a".concat("b").substr(1, 1));
      out_string("/");
      out_int(in_int());
      out_string("/");
      out_string(in_string());
    }
  };
};
