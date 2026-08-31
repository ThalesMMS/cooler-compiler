class Main inherits IO {
  main() : Object {
    let text : String <- "compiler" in {
      out_string(text.concat(" lab\n"));
      out_string("length = ");
      out_int(text.length());
      out_string("\n");
      out_string(text.substr(0, 4).concat("\n"));
    }
  };
};
