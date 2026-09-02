class Base {
  label() : String { "Base" };
  inherited_call() : String { label() };
};
class Child inherits Base {
  label() : String { "Child" };
};
class Main inherits IO {
  count : Int <- 0;
  receiver() : SELF_TYPE {{ out_string("R"); self; }};
  mark(label : String, value : Int) : Int {{ out_string(label); count <- count + 1; value; }};
  combine(first : Int, second : Int) : Int { first * 10 + second };
  main() : Object {{
    out_string((new Child).inherited_call()); out_string("/");
    out_string((new Child)@Base.label()); out_string("/");
    out_int(receiver().combine(mark("A", 1), mark("B", 2))); out_string("/");
    out_int(mark("L", 3) + mark("Q", 4)); out_string("/"); out_int(count);
  }};
};
