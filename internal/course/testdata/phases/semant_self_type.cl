class Box {
  value : Int <- 7;
  clone() : SELF_TYPE { copy() };
  get() : Int { value };
};

class Main inherits IO {
  main() : Object {
    let original : Box <- new Box, duplicate : Box in {
      duplicate <- original.clone();
      out_int(duplicate.get());
      out_string("\n");
      out_string(duplicate.type_name().concat("\n"));
    }
  };
};
