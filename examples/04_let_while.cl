class Main inherits IO {
  main() : Object {
    let current : Int <- 5, total : Int <- 0 in {
      while 0 < current loop {
        total <- total + current;
        current <- current - 1;
      } pool;
      out_string("sum = ");
      out_int(total);
      out_string("\n");
    }
  };
};
