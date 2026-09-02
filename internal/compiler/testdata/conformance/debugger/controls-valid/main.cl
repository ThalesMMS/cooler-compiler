class Helper {
  twice(v : Int) : Int { v + v };
};

class Main inherits IO {
  value : Int <- 1;
  main() : Object {{
    out_int((new Helper).twice(value));
    out_string("\n");
  }};
};
